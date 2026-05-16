package topics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DSIPClient calls the DoD SBIR/STTR Innovation Portal (DSIP) public topic
// search API. Endpoint reverse-engineered from the React app at
// https://www.dodsbirsttr.mil/topics-app/ — the main bundle calls
// /topics/api/public/topics/search with `searchParam` in the body.
//
// This API is undocumented and brittle. The poller treats every error as
// non-fatal: SBIR.gov is the primary source, DSIP is a bonus.
type DSIPClient struct {
	HTTP *http.Client
}

// DSIPTopic is one entry from the DSIP topic search response. The upstream
// schema has 30+ fields; we type only those we persist or display.
type DSIPTopic struct {
	TopicID            string `json:"topicId"`
	TopicCode          string `json:"topicCode"`
	TopicTitle         string `json:"topicTitle"`
	TopicStatus        string `json:"topicStatus"`
	Program            string `json:"program"`        // "SBIR" / "STTR"
	Component          string `json:"component"`      // "USAF" / "NAVY" / "ARMY" / ...
	Command            string `json:"command"`        // sub-component, e.g. "AFMC"
	SolicitationNumber string `json:"solicitationNumber"`
	SolicitationTitle  string `json:"solicitationTitle"`
	CycleName          string `json:"cycleName"`
	// Dates are unix millis.
	TopicStartDate         int64 `json:"topicStartDate"`
	TopicEndDate           int64 `json:"topicEndDate"`
	TopicPreReleaseStart   int64 `json:"topicPreReleaseStartDate"`
	TopicPreReleaseEnd     int64 `json:"topicPreReleaseEndDate"`
}

// dsipSearchResponse is the envelope returned by /public/topics/search.
type dsipSearchResponse struct {
	Total int         `json:"total"`
	Data  []DSIPTopic `json:"data"`
	// Error envelope returned on failure — `errorMessages` is the field set
	// by the upstream when something goes wrong (auth, throttle, etc.).
	ErrorURL      string   `json:"errorURL,omitempty"`
	ErrorMessages []string `json:"errorMessages,omitempty"`
}

// dsipReleaseStatusOpen is the `topicReleaseStatus` lookup value for "Open".
// Discovered via GET /core/api/public/dropdown/lookup?type=topics.release_status.
const dsipReleaseStatusOpen = 591

// Search hits /topics/api/public/topics/search and returns one page of open
// topics. Pagination is offset/size — typical size is 25; the upstream caps
// it implicitly somewhere around a few hundred.
func (c *DSIPClient) Search(ctx context.Context, page, size int) ([]DSIPTopic, error) {
	body := map[string]any{
		"searchText":              nil,
		"components":              nil,
		"program":                 nil,
		"topicReleaseStatus":      []int{dsipReleaseStatusOpen},
		"technologyAreaIds":       nil,
		"modernizationPriorities": nil,
		"solicitationCycleNames":  nil,
		"releaseNumbers":          nil,
	}
	buf, _ := json.Marshal(body)

	u := fmt.Sprintf(
		"https://www.dodsbirsttr.mil/topics/api/public/topics/search?size=%d&page=%d&sortBy=finalTopicCode,asc",
		size, page,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://www.dodsbirsttr.mil")
	req.Header.Set("Referer", "https://www.dodsbirsttr.mil/topics-app/")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dsip search: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dsip search: status %d: %s",
			resp.StatusCode, truncate(string(respBody), 256))
	}

	var r dsipSearchResponse
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("dsip decode: %w (body: %s)", err, truncate(string(respBody), 256))
	}
	if len(r.ErrorMessages) > 0 {
		return nil, fmt.Errorf("dsip error: %s", strings.Join(r.ErrorMessages, "; "))
	}
	// Belt-and-suspenders: the upstream is known to return Closed rows when
	// the body filter is rejected. Drop anything that isn't Open here so
	// callers can trust the returned list.
	out := r.Data[:0]
	for _, t := range r.Data {
		if strings.EqualFold(t.TopicStatus, "Open") || strings.EqualFold(t.TopicStatus, "Pre-Release") {
			out = append(out, t)
		}
	}
	return out, nil
}

// dsipTime converts DSIP's unix-millis timestamps to *time.Time. Returns nil
// for 0 / missing values so the database column stays NULL.
func dsipTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

// dsipURL builds the dashboard URL for a topic. The DSIP web app uses the
// topicId UUID, not the human-readable topicCode.
func dsipURL(topicID string) string {
	if topicID == "" {
		return "https://www.dodsbirsttr.mil/topics-app/"
	}
	return "https://www.dodsbirsttr.mil/topics-app/#/topics/" + topicID
}
