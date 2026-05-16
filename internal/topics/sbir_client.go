// Package topics polls public SBIR/STTR open-topic catalogs and inserts new
// matches into the `topic` table. Two upstream sources are supported:
//
//   - SBIR.gov public API (https://api.www.sbir.gov/public/api/solicitations)
//   - DoD SBIR/STTR Innovation Portal (DSIP) at dodsbirsttr.mil
//
// The SBIR.gov client is the primary source. The DSIP client is best-effort
// — if it returns an error the poll continues with whatever SBIR.gov returned.
package topics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// userAgent is sent on every request. SBIR.gov 403s the default Go user-agent.
const userAgent = "sam-monitor/1.0 (+https://github.com/yieldllc/sam-monitor)"

// SBIRClient calls the public SBIR.gov solicitations API. APIKey is optional —
// SBIR.gov does not require auth — but if present it is appended to every
// request as `api_key=<key>`, matching the SAM client's behaviour so the same
// env var can be reused.
type SBIRClient struct {
	APIKey string
	HTTP   *http.Client
}

// SBIRTopic is one entry in a solicitation's `solicitation_topics` array.
type SBIRTopic struct {
	TopicTitle       string         `json:"topic_title"`
	Branch           string         `json:"branch"`
	TopicNumber      string         `json:"topic_number"`
	TopicDescription string         `json:"topic_description"`
	SBIRTopicLink    string         `json:"sbir_topic_link"`
	Subtopics        []SBIRSubtopic `json:"subtopics"`
}

// SBIRSubtopic is one entry in a topic's `subtopics` array.
type SBIRSubtopic struct {
	SubtopicTitle       string `json:"subtopic_title"`
	Branch              string `json:"branch"`
	SubtopicNumber      string `json:"subtopic_number"`
	SubtopicDescription string `json:"subtopic_description"`
}

// SBIRSolicitation is one record returned by /public/api/solicitations.
// Field names follow the SBIR.gov JSON. Only the fields we persist are typed —
// extras are preserved in raw JSONB at the poller layer.
type SBIRSolicitation struct {
	SolicitationTitle     string      `json:"solicitation_title"`
	SolicitationNumber    string      `json:"solicitation_number"`
	Program               string      `json:"program"`
	Phase                 string      `json:"phase"`
	Agency                string      `json:"agency"`
	Branch                string      `json:"branch"`
	SolicitationYear      any         `json:"solicitation_year"`
	ReleaseDate           string      `json:"release_date"`
	OpenDate              string      `json:"open_date"`
	CloseDate             string      `json:"close_date"`
	ApplicationDueDate    []string    `json:"application_due_date"`
	OccurrenceNumber      any         `json:"occurrence_number"`
	SolicitationAgencyURL string      `json:"solicitation_agency_url"`
	CurrentStatus         string      `json:"current_status"`
	SolicitationTopics    []SBIRTopic `json:"solicitation_topics"`
}

// SBIRAgencies is the closed set of agency codes the SBIR.gov API accepts.
// DOD covers Army/Navy/Air Force/USSF/DARPA via branch; HHS covers NIH.
var SBIRAgencies = []string{"DOD", "HHS", "NASA", "NSF", "DOE", "DHS"}

// Search fetches one page of solicitations for the given (keyword, agency)
// pair. Either may be empty. open=1 is always set: the poller never wants
// closed topics. rows ∈ [1, 50] per docs; we use 50.
func (c *SBIRClient) Search(ctx context.Context, keyword, agency string, start int) ([]SBIRSolicitation, error) {
	v := url.Values{}
	v.Set("open", "1")
	v.Set("rows", "50")
	if start > 0 {
		v.Set("start", fmt.Sprintf("%d", start))
	}
	if keyword != "" {
		v.Set("keyword", keyword)
	}
	if agency != "" {
		v.Set("agency", agency)
	}
	if c.APIKey != "" {
		v.Set("api_key", c.APIKey)
	}
	u := "https://api.www.sbir.gov/public/api/solicitations?" + v.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sbir.gov search: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sbir.gov search: status %d: %s",
			resp.StatusCode, truncate(string(body), 256))
	}

	// SBIR.gov returns an array on success. On rate-limit / maintenance it
	// returns a `{"Code":"TooManyRequestsError",...}` object even with 200.
	// Detect the object form and surface it as a normal error so the caller
	// can log and continue.
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	if strings.HasPrefix(trimmed, "{") {
		var errObj struct {
			Code, Message string
		}
		_ = json.Unmarshal(body, &errObj)
		if errObj.Code != "" {
			return nil, fmt.Errorf("sbir.gov %s: %s", errObj.Code, errObj.Message)
		}
		// 200 + object but no Code: unexpected. Return what we have so the
		// caller can log; don't panic on type mismatch.
		return nil, fmt.Errorf("sbir.gov: unexpected object response: %s", truncate(string(body), 256))
	}

	var sols []SBIRSolicitation
	if err := json.Unmarshal(body, &sols); err != nil {
		return nil, fmt.Errorf("sbir.gov decode: %w (body: %s)", err, truncate(string(body), 256))
	}
	return sols, nil
}

// parseSBIRDate accepts the SBIR.gov date encodings seen in the wild. The API
// commonly returns ISO 8601, MM/DD/YYYY, or YYYY-MM-DD. Returns nil for empty
// or unparseable input.
func parseSBIRDate(s string) *time.Time {
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
		"01/02/2006",
		"1/2/2006",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
