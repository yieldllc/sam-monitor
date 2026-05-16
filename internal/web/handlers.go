// Package web is the HTTP layer: a small server-rendered UI for browsing
// opportunities, marking status, viewing prime POCs, viewing saved searches,
// and viewing registration tracking.
//
// Routing uses stdlib http.ServeMux with the Go 1.22 method+path pattern
// syntax (e.g. "GET /opp/{id}"). Templates are embedded and rendered with
// html/template; each page is parsed as a separate template set cloned from
// the shared layout so that page-level `{{define "content"}}` blocks don't
// collide.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed templates/*.html
var tplFS embed.FS

// pageNames is the list of standalone page templates. Each is cloned from the
// base set (layout + statusPill partial + topicPill partial) at startup so
// their {{define}} blocks stay scoped.
var pageNames = []string{"list.html", "opp.html", "primes.html", "searches.html", "status.html", "topics.html"}

// validStatus is the closed set of opportunity statuses.
var validStatus = map[string]bool{
	"new": true, "interested": true, "pursuing": true, "submitted": true, "ignore": true,
}

// StatusOptions in display order.
var StatusOptions = []string{"new", "interested", "pursuing", "submitted", "ignore"}

// validTopicStatus is the closed set of SBIR/STTR topic statuses.
var validTopicStatus = map[string]bool{
	"new": true, "reviewing": true, "submitted": true, "closed": true,
}

// TopicStatusOptions in display order.
var TopicStatusOptions = []string{"new", "reviewing", "submitted", "closed"}

// Server holds the DB pool and precompiled templates.
type Server struct {
	DB    *pgxpool.Pool
	pages map[string]*template.Template
}

// New builds a Server, parsing the shared "layout" + "statusPill" partial and
// cloning a fresh template set for each page so per-page {{define}}s don't
// stomp on each other.
func New(db *pgxpool.Pool) (*Server, error) {
	base, err := template.ParseFS(tplFS,
		"templates/layout.html",
		"templates/status_pill.html",
		"templates/topic_pill.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse base: %w", err)
	}
	pages := make(map[string]*template.Template, len(pageNames))
	for _, name := range pageNames {
		clone, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone base for %s: %w", name, err)
		}
		t, err := clone.ParseFS(tplFS, "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		pages[name] = t
	}
	return &Server{DB: db, pages: pages}, nil
}

// Routes registers the UI handlers on mux. Go 1.22 method+path patterns let
// us dispatch without a third-party router.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", s.list)
	mux.HandleFunc("GET /opp/{id}", s.detail)
	mux.HandleFunc("POST /opp/{id}/status", s.setStatus)
	mux.HandleFunc("GET /primes", s.primes)
	mux.HandleFunc("GET /searches", s.searches)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /topics", s.topics)
	mux.HandleFunc("POST /topics/{id}/status", s.setTopicStatus)
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, page, data); err != nil {
		slog.Error("render", "page", page, "err", err)
		// Don't double-write headers; the body is already partially sent in
		// most cases. Just log.
	}
}

// ---------------------------------------------------------------------------
// Opportunity list
// ---------------------------------------------------------------------------

type Opp struct {
	NoticeID       string
	SolicitationNo string
	Title          string
	Agency         string
	NAICS          []string
	SetAside       string
	NoticeType     string
	PostedAt       *time.Time
	ResponseDueAt  *time.Time
	URL            string
	Description    string
	Status         string
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	q := `SELECT notice_id, COALESCE(solicitation_no,''), title, COALESCE(agency,''),
	             COALESCE(naics, ARRAY[]::TEXT[]), COALESCE(set_aside,''),
	             COALESCE(notice_type,''), posted_at, response_due_at,
	             COALESCE(url,''), status
	      FROM opportunity`
	var args []any
	if status != "" {
		q += ` WHERE status = $1`
		args = append(args, status)
	}
	q += ` ORDER BY posted_at DESC NULLS LAST, first_seen_at DESC LIMIT 500`

	rows, err := s.DB.Query(r.Context(), q, args...)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer rows.Close()

	var opps []Opp
	for rows.Next() {
		var o Opp
		if err := rows.Scan(&o.NoticeID, &o.SolicitationNo, &o.Title, &o.Agency, &o.NAICS,
			&o.SetAside, &o.NoticeType, &o.PostedAt, &o.ResponseDueAt, &o.URL, &o.Status); err != nil {
			slog.Warn("list scan", "err", err)
			continue
		}
		opps = append(opps, o)
	}

	s.render(w, "list.html", struct {
		Title  string
		Opps   []Opp
		Status string
	}{Title: "Opportunities", Opps: opps, Status: status})
}

// ---------------------------------------------------------------------------
// Opportunity detail + status update
// ---------------------------------------------------------------------------

func (s *Server) loadOpp(ctx context.Context, id string) (*Opp, error) {
	var o Opp
	err := s.DB.QueryRow(ctx, `
		SELECT notice_id, COALESCE(solicitation_no,''), title, COALESCE(agency,''),
		       COALESCE(naics, ARRAY[]::TEXT[]), COALESCE(set_aside,''),
		       COALESCE(notice_type,''), posted_at, response_due_at,
		       COALESCE(url,''), COALESCE(description,''), status
		FROM opportunity WHERE notice_id = $1
	`, id).Scan(&o.NoticeID, &o.SolicitationNo, &o.Title, &o.Agency, &o.NAICS,
		&o.SetAside, &o.NoticeType, &o.PostedAt, &o.ResponseDueAt,
		&o.URL, &o.Description, &o.Status)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	opp, err := s.loadOpp(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, err)
		return
	}
	s.render(w, "opp.html", struct {
		Title         string
		Opp           *Opp
		StatusOptions []string
	}{Title: opp.Title, Opp: opp, StatusOptions: StatusOptions})
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")
	if !validStatus[status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	tag, err := s.DB.Exec(r.Context(),
		`UPDATE opportunity SET status = $1 WHERE notice_id = $2`, status, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		http.NotFound(w, r)
		return
	}
	// HTMX partial: return the new status pill.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t := s.pages["opp.html"] // any page works; statusPill is shared
	if err := t.ExecuteTemplate(w, "statusPill", Opp{Status: status}); err != nil {
		slog.Error("render statusPill", "err", err)
	}
}

// ---------------------------------------------------------------------------
// Primes
// ---------------------------------------------------------------------------

type Prime struct {
	ID           string
	Prime        string
	ContactName  string
	ContactEmail string
	ContactURL   string
	Programs     []string
	Notes        string
	AddedAt      time.Time
}

func (s *Server) primes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `
		SELECT id::text, prime, COALESCE(contact_name,''), COALESCE(contact_email,''),
		       COALESCE(contact_url,''), COALESCE(programs, ARRAY[]::TEXT[]),
		       COALESCE(notes,''), added_at
		FROM prime_poc
		ORDER BY prime
	`)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer rows.Close()
	var primes []Prime
	for rows.Next() {
		var p Prime
		if err := rows.Scan(&p.ID, &p.Prime, &p.ContactName, &p.ContactEmail,
			&p.ContactURL, &p.Programs, &p.Notes, &p.AddedAt); err != nil {
			slog.Warn("primes scan", "err", err)
			continue
		}
		primes = append(primes, p)
	}
	s.render(w, "primes.html", struct {
		Title  string
		Primes []Prime
	}{Title: "Primes", Primes: primes})
}

// ---------------------------------------------------------------------------
// Saved searches
// ---------------------------------------------------------------------------

type Search struct {
	ID           string
	Name         string
	Enabled      bool
	QueryPretty  string
	LastPolledAt *time.Time
	CreatedAt    time.Time
}

func (s *Server) searches(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `
		SELECT id::text, name, query::text, enabled, last_polled_at, created_at
		FROM saved_search
		ORDER BY name
	`)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer rows.Close()
	var searches []Search
	for rows.Next() {
		var (
			sr      Search
			rawJSON string
		)
		if err := rows.Scan(&sr.ID, &sr.Name, &rawJSON, &sr.Enabled,
			&sr.LastPolledAt, &sr.CreatedAt); err != nil {
			slog.Warn("searches scan", "err", err)
			continue
		}
		sr.QueryPretty = prettyJSON(rawJSON)
		searches = append(searches, sr)
	}
	s.render(w, "searches.html", struct {
		Title    string
		Searches []Search
	}{Title: "Saved Searches", Searches: searches})
}

func prettyJSON(s string) string {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Registration status
// ---------------------------------------------------------------------------

type Entity struct {
	UEI         string
	Name        string
	Status      string
	CAGE        string
	RegDate     *time.Time
	ExpDate     *time.Time
	LastChecked *time.Time
}

type StatusEvent struct {
	UEI        string
	OldStatus  string
	NewStatus  string
	NewCAGE    string
	ObservedAt time.Time
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	entityRows, err := s.DB.Query(r.Context(), `
		SELECT uei, COALESCE(name,''), COALESCE(last_status,''), COALESCE(last_cage,''),
		       last_registration_date, last_expiration_date, last_checked_at
		FROM tracked_entity
		ORDER BY name, uei
	`)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer entityRows.Close()
	var entities []Entity
	for entityRows.Next() {
		var e Entity
		if err := entityRows.Scan(&e.UEI, &e.Name, &e.Status, &e.CAGE,
			&e.RegDate, &e.ExpDate, &e.LastChecked); err != nil {
			slog.Warn("status scan entity", "err", err)
			continue
		}
		entities = append(entities, e)
	}

	eventRows, err := s.DB.Query(r.Context(), `
		SELECT uei, COALESCE(old_status,''), COALESCE(new_status,''),
		       COALESCE(new_cage,''), observed_at
		FROM status_event
		ORDER BY observed_at DESC
		LIMIT 50
	`)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer eventRows.Close()
	var events []StatusEvent
	for eventRows.Next() {
		var ev StatusEvent
		if err := eventRows.Scan(&ev.UEI, &ev.OldStatus, &ev.NewStatus, &ev.NewCAGE, &ev.ObservedAt); err != nil {
			slog.Warn("status scan event", "err", err)
			continue
		}
		events = append(events, ev)
	}

	s.render(w, "status.html", struct {
		Title    string
		Entities []Entity
		Events   []StatusEvent
	}{Title: "Registration Status", Entities: entities, Events: events})
}

// ---------------------------------------------------------------------------
// SBIR/STTR topics
// ---------------------------------------------------------------------------

// Topic is the row shape rendered on the /topics list.
type Topic struct {
	ID          string
	Source      string
	TopicCode   string
	Title       string
	Agency      string
	Phase       string
	OpenAt      *time.Time
	CloseAt     *time.Time
	URL         string
	Keywords    []string
	Status      string
	LastSeenAt  time.Time
	DaysToClose int  // negative when closed; computed at render time
	Urgent      bool // CloseAt within 14 days
}

func (s *Server) topics(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(r.Context(), `
		SELECT id::text, source, topic_code, title, COALESCE(agency,''),
		       COALESCE(phase,''), open_at, close_at, COALESCE(url,''),
		       COALESCE(keywords_hit, ARRAY[]::TEXT[]), status, last_seen_at
		FROM topic
		ORDER BY (close_at IS NULL), close_at ASC, last_seen_at DESC
		LIMIT 500
	`)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer rows.Close()

	now := time.Now()
	var topics []Topic
	for rows.Next() {
		var t Topic
		if err := rows.Scan(&t.ID, &t.Source, &t.TopicCode, &t.Title, &t.Agency,
			&t.Phase, &t.OpenAt, &t.CloseAt, &t.URL, &t.Keywords, &t.Status, &t.LastSeenAt); err != nil {
			slog.Warn("topics scan", "err", err)
			continue
		}
		if t.CloseAt != nil {
			t.DaysToClose = int(t.CloseAt.Sub(now).Hours() / 24)
			t.Urgent = t.DaysToClose >= 0 && t.DaysToClose < 14
		}
		topics = append(topics, t)
	}

	s.render(w, "topics.html", struct {
		Title         string
		Topics        []Topic
		StatusOptions []string
	}{Title: "SBIR/STTR Topics", Topics: topics, StatusOptions: TopicStatusOptions})
}

func (s *Server) setTopicStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")
	if !validTopicStatus[status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	tag, err := s.DB.Exec(r.Context(),
		`UPDATE topic SET status = $1 WHERE id = $2`, status, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		http.NotFound(w, r)
		return
	}
	// HTMX partial: return just the pill.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t := s.pages["topics.html"]
	if err := t.ExecuteTemplate(w, "topicPill", Topic{Status: status}); err != nil {
		slog.Error("render topicPill", "err", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *Server) serverError(w http.ResponseWriter, err error) {
	slog.Error("handler error", "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
