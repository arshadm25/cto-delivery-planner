package api

import (
	"strings"
	"testing"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
)

// In the fake Redmine, project 11 is a subproject of 10. Each holds six
// issues, four of them open.

func TestParentForecastCoversSubprojects(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)

	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	d := p.Project.Delivery
	if len(d.CoversProjectIDs) != 2 || d.Inputs["open_issues"] != 8.0 {
		t.Fatalf("parent delivery %+v", d)
	}
	// A viewer who cannot see the subproject gets the parent's own forecast.
	var own struct{ Project ProjectSummary }
	pm.do("GET", "/api/v1/projects/10", nil, &own)
	if d := own.Project.Delivery; len(d.CoversProjectIDs) != 0 || d.Inputs["open_issues"] != 4.0 {
		t.Fatalf("pm sees hidden subproject work: %+v", d)
	}
}

func TestBaselineFreezesScope(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	var c planning.Commitment
	cto.do("POST", "/api/v1/commitments", map[string]any{"project_id": 10, "title": "Go-live", "baseline_date": "2027-01-29", "reason": "SOW"}, &c)
	cto.do("POST", "/api/v1/commitments/"+itoa64(c.ID)+"/decision", map[string]any{"approve": true}, nil)

	var task struct{ ID int }
	if st := cto.do("POST", "/api/v1/projects/10/tasks", map[string]any{"subject": "Client sign-off", "changes": map[string]any{"estimated_hours": 3}}, &task); st != 201 || task.ID >= 0 {
		t.Fatalf("create task %d %+v", st, task)
	}
	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	sc := p.Project.Delivery.Scope
	if sc == nil || sc.BaselineIssues != 12 || sc.CurrentIssues != 13 || len(sc.Added) != 1 || sc.Added[0] != task.ID {
		t.Fatalf("scope %+v", sc)
	}
	if sc.CurrentHours-sc.BaselineHours != 3 {
		t.Fatalf("hours %v -> %v", sc.BaselineHours, sc.CurrentHours)
	}
}

func TestPlannerTasks(t *testing.T) {
	e := newEnv(t)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)
	var ms struct{ ID int }
	pm.do("POST", "/api/v1/projects/10/planner-milestones", map[string]any{"name": "UAT"}, &ms)
	var task struct{ ID int }
	if st := pm.do("POST", "/api/v1/projects/10/tasks", map[string]any{"subject": "Prepare UAT data",
		"changes": map[string]any{"estimated_hours": 5, "assignee_id": 8, "version_id": ms.ID}}, &task); st != 201 {
		t.Fatalf("create %d", st)
	}
	if st := pm.do("POST", "/api/v1/projects/10/tasks", map[string]any{"subject": "  "}, nil); st != 400 {
		t.Fatalf("empty subject %d", st)
	}
	if st := pm.do("POST", "/api/v1/projects/11/tasks", map[string]any{"subject": "x"}, nil); st != 404 {
		t.Fatalf("other project %d", st)
	}
	find := func(status string) *IssueView {
		var out struct{ Issues []IssueView }
		pm.do("GET", "/api/v1/projects/10/issues?status="+status, nil, &out)
		for i := range out.Issues {
			if out.Issues[i].ID == task.ID {
				return &out.Issues[i]
			}
		}
		return nil
	}
	is := find("open")
	if is == nil || !is.PlannerTask || is.RedmineURL != "" || is.Estimated == nil || *is.Estimated != 5 || is.VersionName == nil || *is.VersionName != "UAT" {
		t.Fatalf("task as issue %+v", is)
	}
	if st := pm.do("PATCH", "/api/v1/projects/10/tasks/"+itoa(-task.ID), map[string]any{"done": true}, nil); st != 204 {
		t.Fatalf("done %d", st)
	}
	if is := find("closed"); is == nil || is.StatusName != "Done" {
		t.Fatalf("closed task %+v", is)
	}
	if st := pm.do("DELETE", "/api/v1/projects/10/tasks/"+itoa(-task.ID), nil, nil); st != 204 {
		t.Fatalf("delete %d", st)
	}
	if find("all") != nil {
		t.Fatal("deleted task still listed")
	}
}

func TestMilestonesForecastInBaselineOrder(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	var m1, m2 struct{ ID int }
	cto.do("POST", "/api/v1/projects/10/planner-milestones", map[string]any{"name": "Phase 1"}, &m1)
	cto.do("POST", "/api/v1/projects/10/planner-milestones", map[string]any{"name": "Phase 2"}, &m2)
	cto.do("POST", "/api/v1/projects/10/plan", map[string]any{"issue_ids": []int{1001, 1002}, "changes": map[string]any{"version_id": m1.ID, "estimated_hours": 8}}, nil)
	cto.do("POST", "/api/v1/projects/10/plan", map[string]any{"issue_ids": []int{1004, 1005}, "changes": map[string]any{"version_id": m2.ID, "estimated_hours": 8}}, nil)
	var a struct{ ID int64 }
	cto.do("POST", "/api/v1/capacity-allocations", map[string]any{"person_id": 8, "period_start": "2026-10-19", "period_end": "2026-11-15",
		"available_hours": 160, "projects": map[string]float64{"10": 80}}, &a)
	cto.do("POST", "/api/v1/capacity-allocations/"+itoa64(a.ID)+"/approve", nil, nil)
	for _, m := range []struct {
		id   int
		date string
	}{{m1.ID, "2026-11-20"}, {m2.ID, "2026-12-18"}} {
		var c planning.Commitment
		cto.do("POST", "/api/v1/commitments", map[string]any{"project_id": 10, "version_id": m.id, "title": "x", "baseline_date": m.date, "reason": "plan"}, &c)
		cto.do("POST", "/api/v1/commitments/"+itoa64(c.ID)+"/decision", map[string]any{"approve": true}, nil)
	}
	var out struct{ Milestones []MilestoneView }
	cto.do("GET", "/api/v1/projects/10/milestones", nil, &out)
	got := map[int]*Delivery{}
	for _, m := range out.Milestones {
		got[m.ID] = m.Delivery
	}
	if got[m1.ID] == nil || got[m1.ID].Inputs["earlier_milestones_hours"] != nil {
		t.Fatalf("first milestone %+v", got[m1.ID])
	}
	if got[m2.ID] == nil || got[m2.ID].Inputs["earlier_milestones_hours"] != 16.0 || !got[m2.ID].Forecast.After(*got[m1.ID].Forecast) {
		t.Fatalf("second milestone %+v", got[m2.ID])
	}
}

func TestQADaysInForecast(t *testing.T) {
	e := newEnv(t)
	cto := e.user("cto@example.test", auth.CTO)
	var rules map[string]any
	cto.do("GET", "/api/v1/settings/rules", nil, &rules)
	rules["qa_days"] = 5
	if st := cto.do("PUT", "/api/v1/settings/rules", rules, nil); st != 200 {
		t.Fatalf("save rules %d", st)
	}
	rules["qa_days"] = -1
	if st := cto.do("PUT", "/api/v1/settings/rules", rules, nil); st != 400 {
		t.Fatalf("negative qa days %d", st)
	}
	var a struct{ ID int64 }
	cto.do("POST", "/api/v1/capacity-allocations", map[string]any{"person_id": 8, "period_start": "2026-10-19", "period_end": "2026-11-15",
		"available_hours": 160, "projects": map[string]float64{"10": 80}}, &a)
	cto.do("POST", "/api/v1/capacity-allocations/"+itoa64(a.ID)+"/approve", nil, nil)
	var p struct{ Project ProjectSummary }
	cto.do("GET", "/api/v1/projects/10", nil, &p)
	d := p.Project.Delivery
	if d.DevDone == nil || d.Forecast == nil || !d.Forecast.After(*d.DevDone) || !strings.Contains(strings.Join(d.Assumptions, " "), "QA") {
		t.Fatalf("delivery %+v", d)
	}
}
