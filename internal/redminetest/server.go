// Package redminetest is an in-memory fake of the Redmine REST endpoints the
// planner reads. It is used by integration tests; all data is fictional.
package redminetest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
)

// Server is a fake Redmine. Change its fields under Lock/Unlock.
type Server struct {
	*httptest.Server
	mu sync.Mutex

	APIKey      string
	MaxLimit    int
	Projects    []redmine.Project
	Statuses    []redmine.IssueStatus
	Versions    []redmine.Version
	Issues      []redmine.Issue
	TimeEntries []redmine.TimeEntry
	// TimeEntriesForbidden makes /time_entries.json answer 403.
	TimeEntriesForbidden bool
	// FailNext makes the next N requests answer 503.
	FailNext int
	// FailProject makes every issues request for that project answer 500.
	FailProject int
	// Requests counts requests by path.
	Requests map[string]int
	// Methods records every non-GET method received (should stay empty).
	Methods []string
}

// New starts a fake server with fictional data and API key "test-key".
func New() *Server {
	s := &Server{APIKey: "test-key", MaxLimit: 25, Requests: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) Lock()   { s.mu.Lock() }
func (s *Server) Unlock() { s.mu.Unlock() }

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Requests[r.URL.Path]++
	if r.Method != http.MethodGet {
		s.Methods = append(s.Methods, r.Method)
		http.Error(w, "read-only fake", http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("X-Redmine-API-Key") != s.APIKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.FailNext > 0 {
		s.FailNext--
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	path := r.URL.Path
	switch {
	case path == "/users/current.json":
		writeJSON(w, map[string]any{"user": map[string]any{"id": 1, "login": "svc-planner", "admin": false}})
	case path == "/issue_statuses.json":
		writeJSON(w, map[string]any{"issue_statuses": s.Statuses})
	case path == "/projects.json":
		items := make([]any, len(s.Projects))
		for i := range s.Projects {
			items[i] = s.Projects[i]
		}
		s.page(w, q, "projects", items)
	case strings.HasPrefix(path, "/projects/") && strings.HasSuffix(path, "/versions.json"):
		id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(path, "/projects/"), "/versions.json"))
		var vs []redmine.Version
		for _, v := range s.Versions {
			if v.Project.ID == id || v.Sharing == "system" {
				vs = append(vs, v)
			}
		}
		writeJSON(w, map[string]any{"versions": vs, "total_count": len(vs)})
	case path == "/issues.json":
		s.issues(w, q)
	case path == "/time_entries.json":
		if s.TimeEntriesForbidden {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		pid, _ := strconv.Atoi(q.Get("project_id"))
		var items []any
		for _, te := range s.TimeEntries {
			if pid == 0 || te.Project.ID == pid {
				items = append(items, te)
			}
		}
		s.page(w, q, "time_entries", items)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) issues(w http.ResponseWriter, q map[string][]string) {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	pid, _ := strconv.Atoi(get("project_id"))
	if pid != 0 && pid == s.FailProject {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if pid != 0 && get("subproject_id") != "!*" {
		http.Error(w, "test fake requires subproject_id=!*", http.StatusBadRequest)
		return
	}
	var since time.Time
	if u := get("updated_on"); u != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimPrefix(u, ">="))
		if err != nil {
			http.Error(w, "bad updated_on", http.StatusUnprocessableEntity)
			return
		}
		since = t
	}
	closed := map[int]bool{}
	for _, st := range s.Statuses {
		closed[st.ID] = st.IsClosed
	}
	withRelations := strings.Contains(get("include"), "relations")
	sorted := append([]redmine.Issue(nil), s.Issues...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var items []any
	for _, is := range sorted {
		if pid != 0 && is.Project.ID != pid {
			continue
		}
		if get("status_id") != "*" && closed[is.Status.ID] {
			continue
		}
		if !since.IsZero() {
			t, _ := time.Parse(time.RFC3339, is.UpdatedOn)
			if t.Before(since) {
				continue
			}
		}
		if !withRelations {
			is.Relations = nil
		}
		items = append(items, is)
	}
	s.page(w, q, "issues", items)
}

func (s *Server) page(w http.ResponseWriter, q map[string][]string, key string, items []any) {
	offset, limit := 0, 25
	if v := q["offset"]; len(v) > 0 {
		offset, _ = strconv.Atoi(v[0])
	}
	if v := q["limit"]; len(v) > 0 {
		limit, _ = strconv.Atoi(v[0])
	}
	if limit > s.MaxLimit {
		limit = s.MaxLimit
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	pageItems := []any{}
	if offset < len(items) {
		pageItems = items[offset:end]
	}
	writeJSON(w, map[string]any{key: pageItems, "total_count": len(items), "offset": offset, "limit": limit})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// Client returns a read-only client for the fake.
func (s *Server) Client() *redmine.Client {
	c, err := redmine.NewClient(redmine.Config{BaseURL: s.URL, APIKey: s.APIKey, PageSize: 100, AllowHTTP: true, MaxRetries: 2, Timeout: 5 * time.Second})
	if err != nil {
		panic(err)
	}
	return c
}

// Seed fills the fake with a small fictional portfolio: projects 10 and 11
// (11 is a subproject of 10) in scope, project 12 out of scope, and n
// issues per in-scope project, every third one closed.
func (s *Server) Seed(n int) {
	s.Lock()
	defer s.Unlock()
	t := "2026-01-05T09:00:00Z"
	s.Statuses = []redmine.IssueStatus{{ID: 1, Name: "New"}, {ID: 2, Name: "In Progress"}, {ID: 5, Name: "Closed", IsClosed: true}}
	s.Projects = []redmine.Project{
		{ID: 10, Name: "Alpha Portal", Identifier: "alpha", Status: 1, CreatedOn: t, UpdatedOn: t},
		{ID: 11, Name: "Alpha Mobile", Identifier: "alpha-mobile", Status: 1, Parent: &redmine.IDName{ID: 10, Name: "Alpha Portal"}, CreatedOn: t, UpdatedOn: t},
		{ID: 12, Name: "Internal Admin", Identifier: "admin", Status: 1, CreatedOn: t, UpdatedOn: t},
	}
	due := "2026-03-31"
	s.Versions = []redmine.Version{
		{ID: 100, Project: redmine.IDName{ID: 10}, Name: "Release 1", Status: "open", Sharing: "none", EffectiveDate: &due, CreatedOn: t, UpdatedOn: t},
		{ID: 101, Project: redmine.IDName{ID: 11}, Name: "Mobile beta", Status: "open", Sharing: "none", CreatedOn: t, UpdatedOn: t},
	}
	s.Issues = nil
	id := 1000
	for _, pid := range []int{10, 11, 12} {
		for i := 0; i < n; i++ {
			id++
			is := redmine.Issue{
				ID: id, Subject: "Fictional task " + strconv.Itoa(id), Project: redmine.IDName{ID: pid},
				Tracker: redmine.IDName{ID: 1, Name: "Task"}, Status: redmine.IDName{ID: 1, Name: "New"},
				Priority: redmine.IDName{ID: 2, Name: "Normal"}, Author: &redmine.IDName{ID: 7, Name: "Asha Example"},
				CreatedOn: t, UpdatedOn: t,
			}
			if i%3 == 2 {
				is.Status = redmine.IDName{ID: 5, Name: "Closed"}
				closed := t
				is.ClosedOn = &closed
			}
			if i%2 == 0 {
				is.AssignedTo = &redmine.IDName{ID: 8, Name: "Ravi Sample"}
				h := 4.0
				is.EstimatedHours = &h
			}
			if pid == 10 {
				is.FixedVersion = &redmine.IDName{ID: 100, Name: "Release 1"}
			}
			s.Issues = append(s.Issues, is)
		}
	}
	// Parent and relation between the first two issues of project 10.
	s.Issues[1].Parent = &struct {
		ID int `json:"id"`
	}{ID: 1001}
	rel := redmine.Relation{ID: 1, IssueID: 1001, IssueToID: 1002, RelationType: "blocks"}
	s.Issues[0].Relations = []redmine.Relation{rel}
	s.Issues[1].Relations = []redmine.Relation{rel}
	s.TimeEntries = []redmine.TimeEntry{
		{ID: 1, Project: redmine.IDName{ID: 10}, Issue: &redmine.IDName{ID: 1001}, User: redmine.IDName{ID: 8, Name: "Ravi Sample"},
			Activity: redmine.IDName{ID: 9, Name: "Development"}, Hours: 2.5, SpentOn: "2026-01-06", CreatedOn: t, UpdatedOn: t},
	}
}

// Touch sets an issue's updated_on and returns false if it does not exist.
func (s *Server) Touch(id int, updatedOn string, mutate func(*redmine.Issue)) bool {
	s.Lock()
	defer s.Unlock()
	for i := range s.Issues {
		if s.Issues[i].ID == id {
			s.Issues[i].UpdatedOn = updatedOn
			if mutate != nil {
				mutate(&s.Issues[i])
			}
			return true
		}
	}
	return false
}

// Delete removes an issue, as if it was deleted in Redmine.
func (s *Server) Delete(id int) {
	s.Lock()
	defer s.Unlock()
	out := s.Issues[:0]
	for _, is := range s.Issues {
		if is.ID != id {
			out = append(out, is)
		}
	}
	s.Issues = out
}
