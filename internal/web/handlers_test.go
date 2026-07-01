package web

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestTemplatesParse ensures every embedded template parses. New does not touch
// the DB during parsing, so a nil pool is fine here.
func TestTemplatesParse(t *testing.T) {
	if _, err := New(nil); err != nil {
		t.Fatalf("New (template parse): %v", err)
	}
}

// TestListRender executes the opportunities list template with both an open and
// an expired notice, catching template execution errors (e.g. a renamed field)
// and confirming the expired-deadline styling and the Due toggle render.
func TestListRender(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	past := time.Now().Add(-48 * time.Hour)
	future := time.Now().Add(48 * time.Hour)
	data := struct {
		Title  string
		Opps   []Opp
		Status string
		Due    string
	}{
		Title:  "Opportunities",
		Status: "",
		Due:    "all",
		Opps: []Opp{
			{NoticeID: "n1", Title: "Open SaaS platform", ResponseDueAt: &future, Status: "new"},
			{NoticeID: "n2", Title: "Stale solicitation", ResponseDueAt: &past, Expired: true, Status: "new"},
			{NoticeID: "n3", Title: "Sources sought, no deadline", Status: "new"},
		},
	}

	var buf bytes.Buffer
	if err := s.pages["list.html"].ExecuteTemplate(&buf, "list.html", data); err != nil {
		t.Fatalf("execute list.html: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"Open SaaS platform", "expired", "All (incl. expired)", "Open &amp; undated"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered list missing %q", want)
		}
	}
}
