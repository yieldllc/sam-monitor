//go:build integration

package sam

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// TestSearch_Live hits the real SAM.gov /opportunities/v2/search API.
// Run with: SAM_API_KEY=xxx go test -tags=integration -v ./internal/sam/
func TestSearch_Live(t *testing.T) {
	key := os.Getenv("SAM_API_KEY")
	if key == "" {
		t.Skip("no SAM_API_KEY")
	}
	c := &Client{APIKey: key, HTTP: &http.Client{Timeout: 20 * time.Second}}
	q := url.Values{}
	q.Set("ncode", "541512")
	q.Set("postedFrom", time.Now().Add(-7*24*time.Hour).Format("01/02/2006"))
	q.Set("postedTo", time.Now().Format("01/02/2006"))
	q.Set("limit", "5")
	r, err := c.Search(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("got %d total opportunities (%d in this page)", r.TotalRecords, len(r.OpportunitiesData))
	if len(r.OpportunitiesData) > 0 {
		t.Logf("first: %s — %s", r.OpportunitiesData[0].NoticeID, r.OpportunitiesData[0].Title)
	}
}

// TestEntity_Live hits the real SAM.gov /entity-information/v4/entities API
// for Yield LLC's UEI. Run with: SAM_API_KEY=xxx go test -tags=integration -v ./internal/sam/
func TestEntity_Live(t *testing.T) {
	key := os.Getenv("SAM_API_KEY")
	if key == "" {
		t.Skip("no SAM_API_KEY")
	}
	c := &Client{APIKey: key, HTTP: &http.Client{Timeout: 20 * time.Second}}
	er, err := c.Entity(context.Background(), "TA9TQJR2GL18")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("UEI=%s name=%q status=%q cage=%q",
		er.UEISAM, er.LegalBusinessName, er.RegistrationStatus, er.CAGECode)
}
