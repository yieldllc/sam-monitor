// Package poller iterates over enabled saved_search rows, translates each one
// into a SAM.gov /opportunities/v2/search query, persists new notices, and
// fires an SMTP alert for every freshly inserted row.
//
// Insert-vs-update is detected via the postgres-specific `xmax = 0` trick on
// the INSERT ... ON CONFLICT statement: xmax is 0 for true inserts and the
// xact id of the conflicting tuple for updates.
package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yieldllc/sam-monitor/internal/alert"
	"github.com/yieldllc/sam-monitor/internal/sam"
)

// Poller wires DB, the SAM client and an optional alerter together. Alerter
// may be nil — alerts are simply skipped in that case.
type Poller struct {
	DB      *pgxpool.Pool
	SAM     *sam.Client
	Alerter *alert.SMTP
}

// SavedSearchQuery is the JSONB schema we store in saved_search.query.
// All fields are optional; empty/missing values mean "no filter for this dimension".
type SavedSearchQuery struct {
	NAICS      []string `json:"naics"`
	Keywords   string   `json:"keywords"`
	SetAside   []string `json:"setAside"`
	NoticeType []string `json:"noticeType"`
}

// PollAll fetches every enabled saved_search and polls it. Errors from one
// search do not abort the others — they are logged and the loop continues.
func (p *Poller) PollAll(ctx context.Context) error {
	rows, err := p.DB.Query(ctx,
		`SELECT id::text, name, query, last_polled_at FROM saved_search WHERE enabled = true`)
	if err != nil {
		return fmt.Errorf("list searches: %w", err)
	}
	defer rows.Close()

	type searchRow struct {
		id, name   string
		queryJSON  []byte
		lastPolled *time.Time
	}
	var searches []searchRow
	for rows.Next() {
		var r searchRow
		if err := rows.Scan(&r.id, &r.name, &r.queryJSON, &r.lastPolled); err != nil {
			slog.Warn("scan saved_search", "err", err)
			continue
		}
		searches = append(searches, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iter searches: %w", err)
	}

	for _, s := range searches {
		if err := p.pollOne(ctx, s.id, s.name, s.queryJSON, s.lastPolled); err != nil {
			slog.Warn("poll search", "name", s.name, "err", err)
		}
	}
	return nil
}

func (p *Poller) pollOne(ctx context.Context, id, name string, queryJSON []byte, lastPolled *time.Time) error {
	var q SavedSearchQuery
	if err := json.Unmarshal(queryJSON, &q); err != nil {
		return fmt.Errorf("decode query: %w", err)
	}

	v := url.Values{}
	for _, code := range q.NAICS {
		v.Add("ncode", code)
	}
	if q.Keywords != "" {
		v.Set("q", q.Keywords)
	}
	for _, sa := range q.SetAside {
		v.Add("typeOfSetAside", sa)
	}
	for _, nt := range q.NoticeType {
		v.Add("ptype", nt)
	}

	// Date window: from last_polled_at (or 30d back on cold start) to today.
	// SAM.gov requires postedFrom/postedTo in MM/dd/yyyy.
	from := time.Now().Add(-30 * 24 * time.Hour)
	if lastPolled != nil {
		from = *lastPolled
	}
	v.Set("postedFrom", from.Format("01/02/2006"))
	v.Set("postedTo", time.Now().Format("01/02/2006"))

	// Paginate through results: SAM caps a single response at 1000, and we
	// cap total ingest at 5000 per search per cycle so a misconfigured filter
	// can't burn our 1000 req/hr API budget.
	const (
		pageSize    = 1000
		maxPerCycle = 5000
	)
	v.Set("limit", fmt.Sprintf("%d", pageSize))

	slog.Info("poll search", "name", name, "from", from.Format("2006-01-02"))

	var (
		inserted []sam.Opportunity
		fetched  int
		total    int
	)
	for offset := 0; ; offset += pageSize {
		v.Set("offset", fmt.Sprintf("%d", offset))
		r, err := p.SAM.Search(ctx, v)
		if err != nil {
			return fmt.Errorf("sam search (offset %d): %w", offset, err)
		}
		total = r.TotalRecords
		page := len(r.OpportunitiesData)
		slog.Info("poll search page",
			"name", name, "offset", offset, "page", page, "total", total)

		for _, opp := range r.OpportunitiesData {
			ins, err := p.upsert(ctx, id, opp)
			if err != nil {
				slog.Warn("upsert opp", "notice_id", opp.NoticeID, "err", err)
				continue
			}
			if ins {
				inserted = append(inserted, opp)
			}
		}
		fetched += page

		if page < pageSize || offset+pageSize >= total {
			break // last page reached
		}
		if fetched >= maxPerCycle {
			slog.Warn("poll search truncated at maxPerCycle",
				"name", name, "fetched", fetched, "total", total)
			break
		}
	}
	slog.Info("poll search done", "name", name, "fetched", fetched, "total", total, "inserted", len(inserted))

	if _, err := p.DB.Exec(ctx, `UPDATE saved_search SET last_polled_at = now() WHERE id = $1`, id); err != nil {
		slog.Warn("update last_polled_at", "name", name, "err", err)
	}

	if len(inserted) > 0 && p.Alerter != nil {
		subj := fmt.Sprintf("SAM.gov — %d new opportunit%s for %q", len(inserted), plural(len(inserted)), name)
		if err := p.Alerter.Send(ctx, subj, formatInsertEmail(name, inserted)); err != nil {
			slog.Warn("send alert", "err", err)
		}
	}

	return nil
}

// upsert inserts (or updates response_due_at on conflict). Returns true iff a
// new row was actually inserted, via the `xmax = 0` trick.
func (p *Poller) upsert(ctx context.Context, savedSearchID string, opp sam.Opportunity) (bool, error) {
	raw, _ := json.Marshal(opp)

	// NAICS and PSC are TEXT[] columns; treat empty strings as no value.
	var naics, psc []string
	if opp.NAICSCode != "" {
		naics = []string{opp.NAICSCode}
	}
	if opp.ClassificationCode != "" {
		psc = []string{opp.ClassificationCode}
	}

	var inserted bool
	err := p.DB.QueryRow(ctx, `
		INSERT INTO opportunity (
			notice_id, solicitation_no, title, agency, naics, psc, set_aside,
			notice_type, posted_at, response_due_at, url, raw, saved_search_id, status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'new')
		ON CONFLICT (notice_id) DO UPDATE
			SET response_due_at = EXCLUDED.response_due_at,
				title           = EXCLUDED.title,
				set_aside       = EXCLUDED.set_aside
		RETURNING (xmax = 0) AS inserted
	`,
		opp.NoticeID,
		nullable(opp.SolicitationNum),
		opp.Title,
		nullable(opp.FullParentPath),
		naics,
		psc,
		nullable(opp.SetAside),
		nullable(opp.Type),
		parseDate(opp.PostedDate),
		parseDate(opp.ResponseDeadline),
		nullable(opp.UILink),
		raw,
		savedSearchID,
	).Scan(&inserted)
	if err != nil {
		return false, err
	}
	return inserted, nil
}

// parseDate accepts SAM.gov's several date encodings and returns *time.Time
// (nil when the input is empty or unparseable, so the column is left NULL).
func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

// nullable returns *string so an empty string becomes SQL NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func formatInsertEmail(searchName string, opps []sam.Opportunity) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<h2>%d new SAM.gov opportunit%s for %q</h2>\n", len(opps), plural(len(opps)), searchName)
	b.WriteString("<ul>\n")
	for _, o := range opps {
		due := o.ResponseDeadline
		if due == "" {
			due = "—"
		}
		fmt.Fprintf(&b,
			`<li><a href="%s"><strong>%s</strong></a><br>Agency: %s · NAICS: %s · Set-aside: %s · Due: %s</li>`+"\n",
			htmlEscape(o.UILink), htmlEscape(o.Title), htmlEscape(o.FullParentPath),
			htmlEscape(o.NAICSCode), htmlEscape(o.SetAside), htmlEscape(due))
	}
	b.WriteString("</ul>\n")
	b.WriteString(`<p style="color:#888;font-size:.9em">— sam-monitor</p>` + "\n")
	return b.String()
}

// htmlEscape is a minimal escape for email body interpolation — enough to avoid
// breaking the HTML when titles contain &, <, or >. (We don't import html/template
// here just to escape four characters.)
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
