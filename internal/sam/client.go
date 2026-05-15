// Package sam is a tiny typed client for two SAM.gov public APIs:
//
//   - /opportunities/v2/search — contract opportunity listings (used by the poller)
//   - /entity-information/v4/entities — entity registration status (used by regstatus)
//
// Auth is a single api.data.gov key passed as the api_key query parameter; the
// caller is responsible for setting Client.APIKey. Rate limit is 1000 req/hr on
// the opportunities API which is generous for our 4h cadence.
package sam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client is a thread-safe SAM.gov API client. Zero value is not usable;
// at minimum set APIKey and HTTP.
type Client struct {
	APIKey string
	HTTP   *http.Client
}

// Opportunity is one record from /opportunities/v2/search. Field tags reflect
// the SAM.gov JSON; field names use Go conventions. Only the fields we persist
// or display are typed — the rest is preserved in raw JSONB by the poller.
type Opportunity struct {
	NoticeID           string `json:"noticeId"`
	SolicitationNum    string `json:"solicitationNumber"`
	Title              string `json:"title"`
	FullParentPath     string `json:"fullParentPathName"`
	NAICSCode          string `json:"naicsCode"`
	ClassificationCode string `json:"classificationCode"`
	Type               string `json:"type"`
	BaseType           string `json:"baseType"`
	PostedDate         string `json:"postedDate"`
	ResponseDeadline   string `json:"responseDeadLine"`
	SetAside           string `json:"typeOfSetAsideDescription"`
	UILink             string `json:"uiLink"`
	Description        string `json:"description"`
}

// SearchResult is the envelope returned by /opportunities/v2/search.
type SearchResult struct {
	TotalRecords      int           `json:"totalRecords"`
	OpportunitiesData []Opportunity `json:"opportunitiesData"`
}

// Search calls /opportunities/v2/search with the provided query values. The
// caller must populate `postedFrom` and `postedTo` (MM/dd/yyyy); api_key is
// added automatically. Returns an error if the response is non-200.
func (c *Client) Search(ctx context.Context, q url.Values) (*SearchResult, error) {
	q.Set("api_key", c.APIKey)
	u := "https://api.sam.gov/opportunities/v2/search?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sam.gov search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("sam.gov search: status %d: %s", resp.StatusCode, string(body))
	}
	var r SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("sam.gov search: decode: %w", err)
	}
	return &r, nil
}

// EntityRegistration is the subset of /entity-information/v4/entities we care
// about: status, CAGE, registration window.
type EntityRegistration struct {
	UEISAM                     string `json:"ueiSAM"`
	LegalBusinessName          string `json:"legalBusinessName"`
	CAGECode                   string `json:"cageCode"`
	RegistrationStatus         string `json:"registrationStatus"`
	RegistrationDate           string `json:"registrationDate"`
	RegistrationExpirationDate string `json:"registrationExpirationDate"`
}

// EntityResult is the envelope returned by /entity-information/v4/entities.
type EntityResult struct {
	TotalRecords int `json:"totalRecords"`
	EntityData   []struct {
		EntityRegistration EntityRegistration `json:"entityRegistration"`
	} `json:"entityData"`
}

// Entity fetches the registration status of a single UEI. Returns a synthetic
// "Pending Publication" status when the entity is not yet publicly indexed
// (HTTP 404 or empty entityData) — this is the state between submission and
// CAGE issuance. Non-404 non-200 responses return an error.
func (c *Client) Entity(ctx context.Context, uei string) (*EntityRegistration, error) {
	v := url.Values{}
	v.Set("api_key", c.APIKey)
	v.Set("ueiSAM", uei)
	v.Set("samRegistered", "Yes")
	v.Set("includeSections", "entityRegistration")
	u := "https://api.sam.gov/entity-information/v4/entities?" + v.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sam.gov entity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return &EntityRegistration{UEISAM: uei, RegistrationStatus: "Pending Publication"}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("sam.gov entity: status %d: %s", resp.StatusCode, string(body))
	}
	var r EntityResult
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("sam.gov entity: decode: %w", err)
	}
	if len(r.EntityData) == 0 {
		return &EntityRegistration{UEISAM: uei, RegistrationStatus: "Pending Publication"}, nil
	}
	er := r.EntityData[0].EntityRegistration
	if er.UEISAM == "" {
		er.UEISAM = uei
	}
	return &er, nil
}
