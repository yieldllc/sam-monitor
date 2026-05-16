package topics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yieldllc/sam-monitor/internal/alert"
)

// Poller is the SBIR/STTR topic poller. Alerter and DSIP may be nil — both
// the SMTP alert step and the DSIP-secondary-source step are best-effort.
type Poller struct {
	DB      *pgxpool.Pool
	SBIR    *SBIRClient
	DSIP    *DSIPClient
	Alerter *alert.SMTP
}

// inserted is what we hand to the alert formatter for the email digest.
type inserted struct {
	Source      string
	TopicCode   string
	Title       string
	Agency      string
	CloseAt     *time.Time
	URL         string
	KeywordsHit []string
}

// PollAll loads the active keyword list, then polls SBIR.gov for each
// keyword × agency permutation, then (best-effort) DSIP. New rows trigger a
// single end-of-cycle SMTP digest. Errors on individual sub-polls are logged
// but do not abort the cycle.
func (p *Poller) PollAll(ctx context.Context) error {
	keywords, err := p.loadKeywords(ctx)
	if err != nil {
		return fmt.Errorf("load keywords: %w", err)
	}
	if len(keywords) == 0 {
		slog.Info("topic poll skipped: no enabled keywords")
		return nil
	}
	slog.Info("topic poll start", "keywords", len(keywords))

	var ins []inserted

	// --- Source A: SBIR.gov ---
	if p.SBIR != nil {
		ins = append(ins, p.pollSBIR(ctx, keywords)...)
	} else {
		slog.Warn("topic poll: no SBIR client configured")
	}

	// --- Source B: DSIP (best-effort) ---
	if p.DSIP != nil {
		dsipIns, err := p.pollDSIP(ctx, keywords)
		if err != nil {
			slog.Warn("topic poll dsip", "err", err)
		} else {
			ins = append(ins, dsipIns...)
		}
	}

	slog.Info("topic poll done", "inserted", len(ins))

	if len(ins) > 0 && p.Alerter != nil {
		subj := fmt.Sprintf("SBIR/STTR — %d new open topic(s) matching your keywords", len(ins))
		if err := p.Alerter.Send(ctx, subj, formatTopicEmail(ins)); err != nil {
			slog.Warn("topic alert send", "err", err)
		}
	}
	return nil
}

func (p *Poller) loadKeywords(ctx context.Context) ([]string, error) {
	rows, err := p.DB.Query(ctx,
		`SELECT keyword FROM topic_keyword WHERE enabled = true ORDER BY keyword`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			slog.Warn("scan topic_keyword", "err", err)
			continue
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// SBIR.gov
// ---------------------------------------------------------------------------

// pollSBIR iterates keyword × agency and upserts every matching topic. The
// SBIR.gov `keyword=` param matches against the solicitation title, so we
// also run a client-side pass over every topic's title+description with the
// full keyword list — that catches solicitations whose title is generic but
// whose topics are very specific (common with broad omnibus BAAs like
// "DoD SBIR 25.4").
func (p *Poller) pollSBIR(ctx context.Context, keywords []string) []inserted {
	var ins []inserted
	// dedupe (source, topic_code) within a cycle so the same topic returned
	// for multiple (kw, agency) tuples doesn't fire multiple inserts.
	seen := make(map[string]bool)

	for _, agency := range SBIRAgencies {
		for _, kw := range keywords {
			sols, err := p.SBIR.Search(ctx, kw, agency, 0)
			if err != nil {
				slog.Warn("sbir search", "agency", agency, "keyword", kw, "err", err)
				continue
			}
			slog.Info("sbir search page",
				"agency", agency, "keyword", kw, "solicitations", len(sols))

			for _, sol := range sols {
				if !strings.EqualFold(sol.CurrentStatus, "open") {
					continue
				}
				for _, topic := range sol.SolicitationTopics {
					code := strings.TrimSpace(topic.TopicNumber)
					if code == "" {
						continue
					}
					key := "sbir.gov\x00" + code
					if seen[key] {
						continue
					}
					seen[key] = true

					hits := matchKeywords(
						topic.TopicTitle+" "+topic.TopicDescription+" "+sol.SolicitationTitle,
						keywords,
					)
					if len(hits) == 0 {
						continue
					}
					row, ok := p.upsertSBIR(ctx, sol, topic, hits)
					if ok {
						ins = append(ins, row)
					}
				}
			}
		}
	}
	return ins
}

func (p *Poller) upsertSBIR(ctx context.Context, sol SBIRSolicitation, topic SBIRTopic, hits []string) (inserted, bool) {
	closeAt := pickCloseDate(sol)
	openAt := parseSBIRDate(sol.OpenDate)

	agency := sol.Agency
	if sol.Branch != "" && sol.Branch != sol.Agency {
		agency = sol.Agency + " / " + sol.Branch
	}
	urlStr := topic.SBIRTopicLink
	if urlStr == "" {
		urlStr = sol.SolicitationAgencyURL
	}

	rawBytes, _ := json.Marshal(map[string]any{
		"solicitation": sol,
		"topic":        topic,
	})

	abstract := topic.TopicDescription

	var ins bool
	err := p.DB.QueryRow(ctx, `
		INSERT INTO topic (
			source, topic_code, title, agency, phase,
			open_at, close_at, abstract, url, keywords_hit, raw, status
		)
		VALUES ('sbir.gov',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'new')
		ON CONFLICT (source, topic_code) DO UPDATE SET
			title         = EXCLUDED.title,
			agency        = EXCLUDED.agency,
			phase         = EXCLUDED.phase,
			open_at       = EXCLUDED.open_at,
			close_at      = EXCLUDED.close_at,
			abstract      = EXCLUDED.abstract,
			url           = EXCLUDED.url,
			keywords_hit  = EXCLUDED.keywords_hit,
			raw           = EXCLUDED.raw,
			last_seen_at  = now()
		RETURNING (xmax = 0) AS inserted
	`,
		topic.TopicNumber,
		topic.TopicTitle,
		nullable(agency),
		nullable(sol.Phase),
		openAt,
		closeAt,
		nullable(abstract),
		nullable(urlStr),
		hits,
		rawBytes,
	).Scan(&ins)
	if err != nil {
		slog.Warn("upsert sbir topic", "topic_code", topic.TopicNumber, "err", err)
		return inserted{}, false
	}
	if !ins {
		return inserted{}, false
	}
	return inserted{
		Source:      "sbir.gov",
		TopicCode:   topic.TopicNumber,
		Title:       topic.TopicTitle,
		Agency:      agency,
		CloseAt:     closeAt,
		URL:         urlStr,
		KeywordsHit: hits,
	}, true
}

// pickCloseDate picks the earliest application due date if present, otherwise
// falls back to close_date. application_due_date is per-topic in some BAAs.
func pickCloseDate(sol SBIRSolicitation) *time.Time {
	var earliest *time.Time
	for _, d := range sol.ApplicationDueDate {
		t := parseSBIRDate(d)
		if t == nil {
			continue
		}
		if earliest == nil || t.Before(*earliest) {
			earliest = t
		}
	}
	if earliest != nil {
		return earliest
	}
	return parseSBIRDate(sol.CloseDate)
}

// ---------------------------------------------------------------------------
// DSIP
// ---------------------------------------------------------------------------

// pollDSIP pages through the open-topic listing. Each page yields up to 50
// rows; we cap at 20 pages = 1000 topics per cycle, far more than any single
// cycle has ever held.
func (p *Poller) pollDSIP(ctx context.Context, keywords []string) ([]inserted, error) {
	const (
		pageSize = 50
		maxPages = 20
	)
	var ins []inserted
	seen := make(map[string]bool)

	for page := 0; page < maxPages; page++ {
		topics, err := p.DSIP.Search(ctx, page, pageSize)
		if err != nil {
			return ins, err
		}
		slog.Info("dsip search page", "page", page, "rows", len(topics))
		if len(topics) == 0 {
			break
		}
		for _, t := range topics {
			code := strings.TrimSpace(t.TopicCode)
			if code == "" || seen[code] {
				continue
			}
			seen[code] = true

			hits := matchKeywords(t.TopicTitle, keywords)
			if len(hits) == 0 {
				continue
			}
			row, ok := p.upsertDSIP(ctx, t, hits)
			if ok {
				ins = append(ins, row)
			}
		}
		if len(topics) < pageSize {
			break
		}
	}
	return ins, nil
}

func (p *Poller) upsertDSIP(ctx context.Context, t DSIPTopic, hits []string) (inserted, bool) {
	rawBytes, _ := json.Marshal(t)
	openAt := dsipTime(t.TopicStartDate)
	if openAt == nil {
		openAt = dsipTime(t.TopicPreReleaseStart)
	}
	closeAt := dsipTime(t.TopicEndDate)

	agency := t.Component
	if t.Command != "" && t.Command != t.Component {
		agency = t.Component + " / " + t.Command
	}
	urlStr := dsipURL(t.TopicID)

	var ins bool
	err := p.DB.QueryRow(ctx, `
		INSERT INTO topic (
			source, topic_code, title, agency, phase,
			open_at, close_at, abstract, url, keywords_hit, raw, status
		)
		VALUES ('dsip',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'new')
		ON CONFLICT (source, topic_code) DO UPDATE SET
			title         = EXCLUDED.title,
			agency        = EXCLUDED.agency,
			phase         = EXCLUDED.phase,
			open_at       = EXCLUDED.open_at,
			close_at      = EXCLUDED.close_at,
			abstract      = EXCLUDED.abstract,
			url           = EXCLUDED.url,
			keywords_hit  = EXCLUDED.keywords_hit,
			raw           = EXCLUDED.raw,
			last_seen_at  = now()
		RETURNING (xmax = 0) AS inserted
	`,
		t.TopicCode,
		t.TopicTitle,
		nullable(agency),
		nullable(t.Program), // 'SBIR' / 'STTR' — closest analog to phase here
		openAt,
		closeAt,
		nullable(""), // DSIP search response doesn't include the abstract
		nullable(urlStr),
		hits,
		rawBytes,
	).Scan(&ins)
	if err != nil {
		slog.Warn("upsert dsip topic", "topic_code", t.TopicCode, "err", err)
		return inserted{}, false
	}
	if !ins {
		return inserted{}, false
	}
	return inserted{
		Source:      "dsip",
		TopicCode:   t.TopicCode,
		Title:       t.TopicTitle,
		Agency:      agency,
		CloseAt:     closeAt,
		URL:         urlStr,
		KeywordsHit: hits,
	}, true
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func formatTopicEmail(rows []inserted) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<h2>%d new SBIR/STTR open topic(s)</h2>\n", len(rows))
	b.WriteString("<ul>\n")
	now := time.Now()
	for _, r := range rows {
		due := "—"
		dueColor := ""
		if r.CloseAt != nil {
			days := int(r.CloseAt.Sub(now).Hours() / 24)
			due = fmt.Sprintf("%s (%d days)", r.CloseAt.Format("2006-01-02"), days)
			if days < 14 {
				dueColor = ` style="color:#a00"`
			}
		}
		fmt.Fprintf(&b,
			`<li><a href="%s"><strong>%s</strong></a><br>`+
				`Source: %s · Code: %s · Agency: %s · Due: <span%s>%s</span> · Keywords: %s</li>`+"\n",
			htmlEscape(r.URL), htmlEscape(r.Title),
			htmlEscape(r.Source), htmlEscape(r.TopicCode),
			htmlEscape(r.Agency), dueColor, htmlEscape(due),
			htmlEscape(strings.Join(r.KeywordsHit, ", ")),
		)
	}
	b.WriteString("</ul>\n")
	b.WriteString(`<p style="color:#888;font-size:.9em">— sam-monitor topics</p>` + "\n")
	return b.String()
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
