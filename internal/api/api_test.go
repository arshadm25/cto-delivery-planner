package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/redminetest"
	"github.com/arshadm25/cto-delivery-planner/internal/scope"
	"github.com/arshadm25/cto-delivery-planner/internal/store"
	"github.com/arshadm25/cto-delivery-planner/internal/syncer"
	"github.com/arshadm25/cto-delivery-planner/internal/testdb"
)

const password = "correct horse battery"

type env struct {
	t    *testing.T
	pool *pgxpool.Pool
	fake *redminetest.Server
	sy   *syncer.Syncer
	srv  *httptest.Server
	as   *auth.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testdb.New(t)
	fake := redminetest.New()
	t.Cleanup(fake.Close)
	fake.Seed(6)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	sy, err := syncer.New(fake.Client(), pool, syncer.Options{Scope: &scope.Scope{ProjectIDs: []int{10, 11}}, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := sy.Run(context.Background(), syncer.Full); err != nil || res.Outcome != "succeeded" {
		t.Fatalf("seed sync: %v %+v", err, res)
	}
	as := auth.NewStore(pool).WithCost(bcrypt.MinCost)
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	h := New(Config{RedmineURL: "https://redmine.example.test", Now: func() time.Time { return now }, SyncStaleAfter: 24 * 365 * time.Hour},
		store.New(pool), as, quiet)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &env{t: t, pool: pool, fake: fake, sy: sy, srv: srv, as: as}
}

func (e *env) user(email string, role auth.Role, projects ...int) *client {
	e.t.Helper()
	u, err := e.as.CreateUser(context.Background(), email, "Test "+string(role), password, role)
	if err != nil {
		e.t.Fatal(err)
	}
	if len(projects) > 0 {
		e.as.SetAssignedProjects(context.Background(), u.ID, projects)
	}
	c := e.anon()
	if st := c.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": password}, nil); st != 200 {
		e.t.Fatalf("login %s: %d", email, st)
	}
	return c
}

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
	csrf bool
}

func (e *env) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: e.t, base: e.srv.URL, hc: &http.Client{Jar: jar}, csrf: true}
}

func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if c.csrf {
		req.Header.Set("X-Requested-With", "planner")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func TestCTOSeesPortfolio(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	var p PortfolioSummary
	if st := cto.do("GET", "/api/v1/portfolio/summary", nil, &p); st != 200 {
		t.Fatalf("status %d", st)
	}
	if p.ProjectsInScope != 2 || len(p.Projects) != 2 {
		t.Fatalf("projects %d", p.ProjectsInScope)
	}
	if p.LastSuccessfulSync == nil {
		t.Fatal("last sync missing")
	}
	total := 0
	for _, ps := range p.Projects {
		total += ps.Issues.Total
	}
	if total != 12 {
		t.Fatalf("issue total %d, want 12 (open and closed)", total)
	}
}

func TestProjectManagerSeesOnlyAssignedProjects(t *testing.T) {
	e := newEnv(t)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)
	var list struct{ Projects []ProjectSummary }
	pm.do("GET", "/api/v1/projects", nil, &list)
	if len(list.Projects) != 1 || list.Projects[0].ID != 10 {
		t.Fatalf("projects %+v", list.Projects)
	}
	if st := pm.do("GET", "/api/v1/projects/10", nil, nil); st != 200 {
		t.Fatalf("own project %d", st)
	}
	for _, path := range []string{"/api/v1/projects/11", "/api/v1/projects/11/issues", "/api/v1/projects/11/milestones"} {
		if st := pm.do("GET", path, nil, nil); st != 404 {
			t.Errorf("%s = %d, want 404", path, st)
		}
	}
	var rk struct{ Flags []FlagView }
	pm.do("GET", "/api/v1/risks?per_page=200", nil, &rk)
	for _, f := range rk.Flags {
		if f.ProjectID != 10 {
			t.Fatalf("risk from project %d leaked", f.ProjectID)
		}
	}
}

func TestViewerWithoutProjectsGetsNothing(t *testing.T) {
	e := newEnv(t)
	v := e.user("viewer@example.test", auth.Viewer)
	var p PortfolioSummary
	v.do("GET", "/api/v1/portfolio/summary", nil, &p)
	if p.ProjectsInScope != 0 || p.OpenIssues != 0 {
		t.Fatalf("viewer sees %d projects", p.ProjectsInScope)
	}
}

func TestAuthAndRoleChecks(t *testing.T) {
	e := newEnv(t)
	if st := e.anon().do("GET", "/api/v1/portfolio/summary", nil, nil); st != 401 {
		t.Fatalf("anonymous %d", st)
	}
	v := e.user("viewer@example.test", auth.Viewer, 10)
	if st := v.do("PUT", "/api/v1/projects/10/roles", map[string]string{"owner": "Someone"}, nil); st != 403 {
		t.Fatalf("viewer edit roles %d", st)
	}
	if st := v.do("GET", "/api/v1/admin/users", nil, nil); st != 403 {
		t.Fatalf("viewer admin %d", st)
	}
	cto := e.user("cto@example.test", auth.CTO)
	cto.csrf = false
	if st := cto.do("PUT", "/api/v1/projects/10/roles", map[string]string{"owner": "Asha Example"}, nil); st != 403 {
		t.Fatalf("missing CSRF header %d", st)
	}
	cto.csrf = true
	if st := cto.do("PUT", "/api/v1/projects/10/roles", map[string]string{"owner": "Asha Example"}, nil); st != 204 {
		t.Fatalf("cto edit roles %d", st)
	}
	var n int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE entity = 'project_roles'`).Scan(&n)
	if n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.as.CreateUser(context.Background(), "a@example.test", "A", password, auth.Viewer)
	c := e.anon()
	for i := 0; i < 10; i++ {
		c.do("POST", "/api/v1/auth/login", map[string]string{"email": "a@example.test", "password": "wrong password!"}, nil)
	}
	if st := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "a@example.test", "password": password}, nil); st != 429 {
		t.Fatalf("status %d, want 429", st)
	}
}

func TestSyncFailureIsVisibleAndPreviousDataStays(t *testing.T) {
	e := newEnv(t)
	e.fake.Lock()
	e.fake.FailProject = 11
	e.fake.Unlock()
	e.sy.Run(context.Background(), syncer.Full)
	cto := e.user("cto@example.test", auth.CTO)
	var st struct {
		LatestFailed    bool `json:"latest_failed"`
		ShowingPrevious bool `json:"showing_previous"`
		Status          struct {
			Runs []struct {
				Outcome      string  `json:"outcome"`
				ErrorSummary *string `json:"error_summary"`
			}
		}
	}
	cto.do("GET", "/api/v1/sync/status", nil, &st)
	if !st.LatestFailed || !st.ShowingPrevious || st.Status.Runs[0].Outcome != "partial" || st.Status.Runs[0].ErrorSummary == nil {
		t.Fatalf("%+v", st)
	}
	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/11", nil, &p)
	if p.Project.Issues.Total != 6 {
		t.Fatalf("project 11 lost data: %d", p.Project.Issues.Total)
	}
}

func TestUnknownsAreReportedHonestly(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	var p struct {
		Project  ProjectSummary
		DataGaps []string `json:"data_gaps"`
	}
	cto.do("GET", "/api/v1/projects/11", nil, &p)
	if p.Project.Owner != nil || p.Project.TechLead != nil {
		t.Fatal("owner should be TBD")
	}
	// Project 11 has open issues without estimates; coverage must be below 1
	// and the gap must be listed, not hidden as zero hours.
	if p.Project.EstimateCoverage == nil || *p.Project.EstimateCoverage >= 1 {
		t.Fatalf("coverage %v", p.Project.EstimateCoverage)
	}
	if len(p.DataGaps) == 0 {
		t.Fatal("no data gaps listed")
	}
}

func TestRepeatedImportDoesNotDuplicate(t *testing.T) {
	e := newEnv(t)
	e.sy.Run(context.Background(), syncer.Full)
	cto := e.user("cto@example.test", auth.CTO)
	var iss struct{ Total int }
	cto.do("GET", "/api/v1/projects/10/issues?status=all", nil, &iss)
	if iss.Total != 6 {
		t.Fatalf("total %d after re-import", iss.Total)
	}
}

func TestIssueFlagsLinkToRedmine(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	var iss struct{ Issues []IssueView }
	cto.do("GET", "/api/v1/projects/10/issues?flag=missing_owner", nil, &iss)
	if len(iss.Issues) == 0 {
		t.Fatal("expected unassigned issues")
	}
	for _, is := range iss.Issues {
		if is.AssigneeID != nil || is.RedmineURL == "" {
			t.Fatalf("%+v", is)
		}
	}
}

func TestSyncHealthEndpoint(t *testing.T) {
	e := newEnv(t)
	c := e.anon()
	if st := c.do("GET", "/health/sync", nil, nil); st != 200 {
		t.Fatalf("healthy %d", st)
	}
	e.fake.Lock()
	e.fake.FailProject = 10
	e.fake.Unlock()
	e.sy.Run(context.Background(), syncer.Full)
	if st := c.do("GET", "/health/sync", nil, nil); st != 503 {
		t.Fatalf("after failure %d", st)
	}
}

func TestAwaitingQABucket(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	rules := map[string]any{"stale_after_days": 14, "milestone_warn_days": 14, "awaiting_qa_statuses": []string{"New"}}
	if st := cto.do("PUT", "/api/v1/settings/rules", rules, nil); st != 200 {
		t.Fatalf("save rules %d", st)
	}
	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	if p.Project.Issues.Open == 0 || p.Project.Issues.AwaitingQA != p.Project.Issues.Open {
		t.Fatalf("open %d awaiting QA %d", p.Project.Issues.Open, p.Project.Issues.AwaitingQA)
	}
	if p.Project.EstimateCoverage != nil || p.Project.Issues.OpenNoEstimate != 0 {
		t.Fatalf("awaiting QA issues must not count as development work: %+v", p.Project)
	}
	var iss struct {
		Total  int
		Issues []IssueView
	}
	cto.do("GET", "/api/v1/projects/10/issues?status=awaiting_qa", nil, &iss)
	if iss.Total != p.Project.Issues.AwaitingQA || !iss.Issues[0].AwaitingQA {
		t.Fatalf("filter total %d", iss.Total)
	}
}
