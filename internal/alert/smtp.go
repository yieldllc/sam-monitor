// Package alert is a thin wrapper around net/smtp for sending HTML emails via
// authenticated SMTP. Designed for Google Workspace / Gmail app-password auth
// on smtp.gmail.com:587 (STARTTLS) but works with any RFC 5321 + AUTH PLAIN
// server.
//
// Configure via env: SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM,
// SMTP_TO. SMTP_TO may be comma-separated. If SMTP_HOST is unset, FromEnv
// returns nil — callers should treat nil as "alerting disabled".
package alert

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"strings"
)

// SMTP holds the connection + envelope settings for an authenticated SMTP send.
type SMTP struct {
	Host, Port string
	User, Pass string
	From, To   string
}

// FromEnv builds an SMTP from environment variables. Returns nil if SMTP_HOST
// is empty — this is the "no alerter configured" path and callers should
// gracefully skip sends.
func FromEnv() *SMTP {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	return &SMTP{
		Host: host,
		Port: port,
		User: os.Getenv("SMTP_USER"),
		Pass: os.Getenv("SMTP_PASS"),
		From: os.Getenv("SMTP_FROM"),
		To:   os.Getenv("SMTP_TO"),
	}
}

// Send delivers an HTML email. The context is logged but net/smtp does not
// support deadlines natively; long-running sends are bounded by the SMTP
// server. SMTP_TO may be comma-separated.
func (s *SMTP) Send(ctx context.Context, subject, bodyHTML string) error {
	if s == nil {
		return nil
	}
	if s.From == "" || s.To == "" {
		return fmt.Errorf("smtp: SMTP_FROM and SMTP_TO must be set")
	}

	recipients := splitAndTrim(s.To)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		s.From, strings.Join(recipients, ", "), subject, bodyHTML,
	)

	auth := smtp.PlainAuth("", s.User, s.Pass, s.Host)
	addr := s.Host + ":" + s.Port

	slog.Info("smtp send", "to", recipients, "subject", subject)
	if err := smtp.SendMail(addr, auth, s.From, recipients, []byte(msg)); err != nil {
		return fmt.Errorf("smtp.SendMail: %w", err)
	}
	return nil
}

func splitAndTrim(csv string) []string {
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
