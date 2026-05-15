// Package regstatus polls SAM.gov entity registration status for every UEI in
// tracked_entity and records changes in status_event. Designed for "is my
// CAGE issued yet?" — when the entity flips from Pending Publication → Active
// (or a CAGE is assigned), an SMTP alert fires and the change is logged.
//
// Cadence is whatever the caller sets (6h ticker in main.go).
package regstatus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yieldllc/sam-monitor/internal/alert"
	"github.com/yieldllc/sam-monitor/internal/sam"
)

// Tracker is the regstatus poller. Alerter may be nil.
type Tracker struct {
	DB      *pgxpool.Pool
	SAM     *sam.Client
	Alerter *alert.SMTP
}

// PollAll iterates over tracked_entity and calls SAM.gov for each UEI. Errors
// on individual entities are logged but do not abort the loop.
func (t *Tracker) PollAll(ctx context.Context) error {
	rows, err := t.DB.Query(ctx,
		`SELECT uei, COALESCE(last_status,''), COALESCE(last_cage,'') FROM tracked_entity`)
	if err != nil {
		return fmt.Errorf("list tracked: %w", err)
	}
	defer rows.Close()

	type row struct {
		uei, lastStatus, lastCAGE string
	}
	var entities []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.uei, &r.lastStatus, &r.lastCAGE); err != nil {
			slog.Warn("scan tracked", "err", err)
			continue
		}
		entities = append(entities, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iter tracked: %w", err)
	}

	for _, e := range entities {
		if err := t.pollOne(ctx, e.uei, e.lastStatus, e.lastCAGE); err != nil {
			slog.Warn("regstatus poll", "uei", e.uei, "err", err)
		}
	}
	return nil
}

func (t *Tracker) pollOne(ctx context.Context, uei, lastStatus, lastCAGE string) error {
	er, err := t.SAM.Entity(ctx, uei)
	if err != nil {
		return err
	}
	slog.Info("regstatus check", "uei", uei, "status", er.RegistrationStatus, "cage", er.CAGECode)

	raw, _ := json.Marshal(er)

	// Update last-known state. We update name too if we now know it.
	_, err = t.DB.Exec(ctx, `
		UPDATE tracked_entity SET
		  name                   = COALESCE(NULLIF($2,''), name),
		  last_status            = NULLIF($3,''),
		  last_cage              = NULLIF($4,''),
		  last_registration_date = $5,
		  last_expiration_date   = $6,
		  last_checked_at        = now()
		WHERE uei = $1
	`, uei,
		er.LegalBusinessName,
		er.RegistrationStatus,
		er.CAGECode,
		parseDate(er.RegistrationDate),
		parseDate(er.RegistrationExpirationDate),
	)
	if err != nil {
		return fmt.Errorf("update tracked_entity: %w", err)
	}

	statusChanged := er.RegistrationStatus != "" && er.RegistrationStatus != lastStatus
	cageChanged := er.CAGECode != "" && er.CAGECode != lastCAGE

	if !statusChanged && !cageChanged {
		return nil
	}

	// Record the transition.
	if _, err := t.DB.Exec(ctx, `
		INSERT INTO status_event (uei, old_status, new_status, old_cage, new_cage, raw)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), $6)
	`, uei, lastStatus, er.RegistrationStatus, lastCAGE, er.CAGECode, raw); err != nil {
		return fmt.Errorf("insert status_event: %w", err)
	}

	if t.Alerter != nil {
		subj := fmt.Sprintf("SAM.gov status change — %s", uei)
		body := formatStatusEmail(uei, lastStatus, lastCAGE, er)
		if err := t.Alerter.Send(ctx, subj, body); err != nil {
			slog.Warn("regstatus send alert", "err", err)
		}
	}
	return nil
}

func formatStatusEmail(uei, oldStatus, oldCAGE string, er *sam.EntityRegistration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<h2>SAM.gov registration update — %s</h2>\n", htmlEscape(uei))
	if er.LegalBusinessName != "" {
		fmt.Fprintf(&b, "<p><strong>%s</strong></p>\n", htmlEscape(er.LegalBusinessName))
	}
	b.WriteString("<table style=\"border-collapse:collapse\"><tr><th>Field</th><th>Before</th><th>Now</th></tr>")
	row := func(label, before, after string) {
		fmt.Fprintf(&b, "<tr><td><strong>%s</strong></td><td>%s</td><td><strong>%s</strong></td></tr>",
			htmlEscape(label), htmlEscape(or(before, "—")), htmlEscape(or(after, "—")))
	}
	row("Status", oldStatus, er.RegistrationStatus)
	row("CAGE", oldCAGE, er.CAGECode)
	if er.RegistrationDate != "" {
		row("Registration date", "", er.RegistrationDate)
	}
	if er.RegistrationExpirationDate != "" {
		row("Expires", "", er.RegistrationExpirationDate)
	}
	b.WriteString("</table>\n")
	b.WriteString(`<p style="color:#888;font-size:.9em">— sam-monitor regstatus</p>` + "\n")
	return b.String()
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

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
