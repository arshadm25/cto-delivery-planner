package redmine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "test-key-not-a-secret"

func newTestClient(t *testing.T, srv *httptest.Server, pageSize int) *Client {
	t.Helper()
	c, err := NewClient(Config{BaseURL: srv.URL, APIKey: testKey, PageSize: pageSize, MaxRetries: 2, AllowHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = func(int) time.Duration { return time.Millisecond }
	return c
}

// issuesServer serves n issues, capping page size at maxLimit like Redmine does.
func issuesServer(t *testing.T, n, maxLimit int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Redmine-API-Key") != testKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.Contains(r.URL.RawQuery, testKey) {
			t.Errorf("API key leaked into query string")
		}
		if r.URL.Query().Get("status_id") != "*" {
			t.Errorf("status_id = %q, want *", r.URL.Query().Get("status_id"))
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit > maxLimit {
			limit = maxLimit
		}
		var issues []map[string]any
		for id := offset + 1; id <= n && id <= offset+limit; id++ {
			issues = append(issues, map[string]any{"id": id, "project": map[string]any{"id": 1, "name": "P"}})
		}
		json.NewEncoder(w).Encode(map[string]any{"issues": issues, "total_count": n, "offset": offset, "limit": limit})
	}))
}

func TestListIssuesPaginatesAndFollowsServerLimit(t *testing.T) {
	srv := issuesServer(t, 250, 100)
	defer srv.Close()
	// Ask for 500 per page; the server caps at 100, so we expect 3 pages.
	c := newTestClient(t, srv, 500)

	issues, stats, err := c.ListIssues(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 250 {
		t.Fatalf("got %d issues, want 250", len(issues))
	}
	if stats.Pages != 3 || stats.ReportedTotal != 250 || stats.Duplicates != 0 {
		t.Fatalf("unexpected stats %+v", stats)
	}
}

func TestEmptyCollection(t *testing.T) {
	srv := issuesServer(t, 0, 100)
	defer srv.Close()
	issues, stats, err := newTestClient(t, srv, 100).ListIssues(context.Background(), nil)
	if err != nil || len(issues) != 0 || stats.Pages != 1 {
		t.Fatalf("issues=%d stats=%+v err=%v", len(issues), stats, err)
	}
}

func TestNonGetRequestsAreRefused(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()
	c := newTestClient(t, srv, 100)

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(m, srv.URL+"/issues/1.json", nil)
		_, err := c.http.Do(req)
		if !errors.Is(err, ErrWriteRefused) {
			t.Errorf("%s: err = %v, want ErrWriteRefused", m, err)
		}
	}
	if hits != 0 {
		t.Fatalf("server received %d write requests", hits)
	}
}

func TestRetriesTransientFailures(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"user":{"id":7,"login":"svc-planner"}}`)
	}))
	defer srv.Close()

	u, err := newTestClient(t, srv, 100).CurrentUser(context.Background())
	if err != nil || u.Login != "svc-planner" || calls != 3 {
		t.Fatalf("user=%+v calls=%d err=%v", u, calls, err)
	}
}

func TestAuthFailureIsNotRetriedAndHidesKey(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv, 100).CurrentUser(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 401 {
		t.Fatalf("err = %v, want 401 StatusError", err)
	}
	if calls != 1 {
		t.Fatalf("401 was retried: %d calls", calls)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("error message contains the API key")
	}
}

func TestDuplicatesAcrossPagesAreCounted(t *testing.T) {
	// Simulates a record shifting between pages during the fetch.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		ids := []int{1, 2}
		if offset == 2 {
			ids = []int{2, 3}
		}
		var issues []map[string]any
		for _, id := range ids {
			issues = append(issues, map[string]any{"id": id})
		}
		json.NewEncoder(w).Encode(map[string]any{"issues": issues, "total_count": 4, "offset": offset, "limit": 2})
	}))
	defer srv.Close()

	_, stats, err := newTestClient(t, srv, 2).ListIssues(context.Background(), nil)
	if err != nil || stats.Duplicates != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestNewClientValidation(t *testing.T) {
	cases := []Config{
		{BaseURL: "", APIKey: "k"},
		{BaseURL: "https://r.example.com", APIKey: ""},
		{BaseURL: "http://r.example.com", APIKey: "k"},
		{BaseURL: "https://user:pass@r.example.com", APIKey: "k"},
	}
	for _, cfg := range cases {
		if _, err := NewClient(cfg); err == nil {
			t.Errorf("NewClient(%q) succeeded, want error", cfg.BaseURL)
		}
	}
	if _, err := NewClient(Config{BaseURL: "https://r.example.com/redmine/", APIKey: "k"}); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestMissingFieldsStayNil(t *testing.T) {
	var is Issue
	if err := json.Unmarshal([]byte(`{"id":1,"project":{"id":2,"name":"P"}}`), &is); err != nil {
		t.Fatal(err)
	}
	if is.EstimatedHours != nil || is.DueDate != nil || is.AssignedTo != nil {
		t.Fatal("missing fields should decode as nil, not zero")
	}
}
