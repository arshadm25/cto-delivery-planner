package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
)

func TestCommitmentApprovalAndRevision(t *testing.T) {
	e := newEnv(t)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)
	cto := e.user("cto@example.test", auth.CTO)

	var c planning.Commitment
	if st := pm.do("POST", "/api/v1/commitments", map[string]any{"project_id": 10, "title": "Go-live", "baseline_date": "2026-12-15", "reason": "Signed SOW"}, &c); st != 201 {
		t.Fatalf("propose %d", st)
	}
	if c.Status != "proposed" {
		t.Fatalf("status %s", c.Status)
	}
	if st := pm.do("POST", "/api/v1/commitments", map[string]any{"project_id": 11, "title": "x", "baseline_date": "2026-12-15", "reason": "x"}, nil); st != 404 {
		t.Fatalf("pm proposing for another project: %d", st)
	}
	if st := pm.do("POST", "/api/v1/commitments/"+itoa64(c.ID)+"/decision", map[string]any{"approve": true}, nil); st != 403 {
		t.Fatalf("pm approving: %d", st)
	}

	// Before approval the project has no baseline.
	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	if p.Project.Delivery == nil || p.Project.Delivery.Baseline != nil {
		t.Fatalf("baseline set before approval: %+v", p.Project.Delivery)
	}

	if st := cto.do("POST", "/api/v1/commitments/"+itoa64(c.ID)+"/decision", map[string]any{"approve": true}, &c); st != 200 || c.Status != "approved" {
		t.Fatalf("approve %d %s", st, c.Status)
	}
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	if p.Project.Delivery.Baseline == nil || p.Project.Delivery.Baseline.Format("2006-01-02") != "2026-12-15" {
		t.Fatalf("baseline %+v", p.Project.Delivery)
	}

	// A revision is a new row; the old baseline is kept as superseded.
	var rev planning.Commitment
	pm.do("POST", "/api/v1/commitments", map[string]any{"project_id": 10, "title": "Go-live", "baseline_date": "2027-01-20", "reason": "Client added scope"}, &rev)
	if rev.SupersedesID == nil || *rev.SupersedesID != c.ID {
		t.Fatalf("revision does not supersede: %+v", rev)
	}
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	if p.Project.Delivery.Baseline.Format("2006-01-02") != "2026-12-15" {
		t.Fatal("a pending revision must not change the baseline")
	}
	cto.do("POST", "/api/v1/commitments/"+itoa64(rev.ID)+"/decision", map[string]any{"approve": true}, nil)
	var list struct{ Commitments []planning.Commitment }
	cto.do("GET", "/api/v1/commitments?project=10", nil, &list)
	statuses := map[int64]string{}
	for _, x := range list.Commitments {
		statuses[x.ID] = x.Status
	}
	if statuses[c.ID] != "superseded" || statuses[rev.ID] != "approved" {
		t.Fatalf("statuses %v", statuses)
	}
	var n int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE entity = 'delivery_commitments'`).Scan(&n)
	if n != 4 {
		t.Fatalf("audit rows %d, want 4", n)
	}
	if st := cto.do("POST", "/api/v1/commitments/"+itoa64(rev.ID)+"/decision", map[string]any{"approve": false}, nil); st != 400 {
		t.Fatalf("re-deciding %d", st)
	}
}

func TestCapacityForecastAndRisk(t *testing.T) {
	e := newEnv(t)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)
	cto := e.user("cto@example.test", auth.CTO)

	// The fake gives project 10 three estimated open issues of 4 h.
	alloc := map[string]any{"person_id": 8, "period_start": "2026-10-19", "period_end": "2026-11-13", "available_hours": 40,
		"assumptions": "Two days a week on this project", "projects": map[string]float64{"10": 4}}
	var created struct{ ID int64 }
	if st := pm.do("POST", "/api/v1/capacity-allocations", alloc, &created); st != 200 {
		t.Fatalf("create %d", st)
	}
	bad := map[string]any{"person_id": 8, "period_start": "2026-10-19", "period_end": "2026-11-13", "available_hours": 40, "projects": map[string]float64{"11": 4}}
	if st := pm.do("POST", "/api/v1/capacity-allocations", bad, nil); st != 403 {
		t.Fatalf("pm allocating to an unassigned project %d", st)
	}
	over := map[string]any{"person_id": 8, "period_start": "2026-10-19", "period_end": "2026-11-13", "available_hours": 2, "projects": map[string]float64{"10": 4}}
	if st := pm.do("POST", "/api/v1/capacity-allocations", over, nil); st != 400 {
		t.Fatalf("allocation above availability %d", st)
	}
	if st := pm.do("POST", "/api/v1/capacity-allocations/"+itoa64(created.ID)+"/approve", nil, nil); st != 403 {
		t.Fatalf("pm approving capacity %d", st)
	}

	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	if p.Project.Delivery.Forecast != nil {
		t.Fatalf("draft capacity must not drive a forecast: %+v", p.Project.Delivery)
	}
	if st := cto.do("POST", "/api/v1/capacity-allocations/"+itoa64(created.ID)+"/approve", nil, nil); st != 204 {
		t.Fatalf("approve %d", st)
	}

	// Baseline tomorrow, which the forecast cannot meet.
	var c planning.Commitment
	pm.do("POST", "/api/v1/commitments", map[string]any{"project_id": 10, "title": "Release", "baseline_date": "2026-10-23", "reason": "Agreed"}, &c)
	cto.do("POST", "/api/v1/commitments/"+itoa64(c.ID)+"/decision", map[string]any{"approve": true}, nil)

	cto.do("GET", "/api/v1/projects/10", nil, &p)
	d := p.Project.Delivery
	if d.Forecast == nil || d.Method != "remaining_effort_over_capacity" || d.Status != "at_risk" {
		t.Fatalf("delivery %+v", d)
	}
	if d.Baseline.Format("2006-01-02") != "2026-10-23" {
		t.Fatal("baseline must stay as approved")
	}
	if len(d.Assumptions) == 0 || d.Inputs["open_issues"] == nil {
		t.Fatal("forecast must explain itself")
	}
	if p.Project.Health.State != "at_risk" && p.Project.Health.State != "blocked" {
		t.Fatalf("health %s", p.Project.Health.State)
	}
	var rk struct{ Flags []FlagView }
	cto.do("GET", "/api/v1/risks?kind=forecast_after_baseline", nil, &rk)
	if len(rk.Flags) != 1 {
		t.Fatalf("forecast flags %d", len(rk.Flags))
	}

	var wl WorkloadView
	cto.do("GET", "/api/v1/workload?from=2026-10-22&to=2026-11-13", nil, &wl)
	if len(wl.People) == 0 || len(wl.Unassigned) == 0 {
		t.Fatalf("workload %+v", wl)
	}
	for i := 1; i < len(wl.People); i++ {
		if wl.People[i-1].PersonName > wl.People[i].PersonName {
			t.Fatal("people must be sorted by name, not ranked")
		}
	}
}

func TestSnapshotsAndWeeklyReport(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	if st := cto.do("POST", "/api/v1/snapshots", nil, nil); st != 200 {
		t.Fatalf("snapshot %d", st)
	}
	// Age the snapshot a week and change the source data.
	e.pool.Exec(context.Background(), `UPDATE project_snapshots SET snapshot_date = snapshot_date - 7, open_issues = open_issues + 3`)

	var rep WeeklyReport
	cto.do("GET", "/api/v1/reports/weekly", nil, &rep)
	if rep.ComparedWith == nil || len(rep.Changes) != 0 && rep.Changes[0].Causes == nil {
		t.Fatalf("report %+v", rep)
	}
	if len(rep.Decisions) == 0 || len(rep.DataQuality) == 0 {
		t.Fatalf("decisions %v data %v", rep.Decisions, rep.DataQuality)
	}

	req, _ := http.NewRequest("GET", cto.base+"/api/v1/reports/weekly?format=md", nil)
	resp, err := cto.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	md := string(body)
	if !strings.Contains(md, "# Weekly delivery report") || !strings.Contains(md, "## Decisions needed") {
		t.Fatalf("markdown:\n%s", md)
	}
	if strings.Contains(md, "Ravi Sample") || strings.Contains(md, "Asha Example") {
		t.Fatal("shared report must not contain people's names")
	}

	var h struct{ Snapshots []planning.Snapshot }
	cto.do("GET", "/api/v1/projects/10/history", nil, &h)
	if len(h.Snapshots) != 1 {
		t.Fatalf("history %d", len(h.Snapshots))
	}
}
