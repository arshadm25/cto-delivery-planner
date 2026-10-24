package api

import (
	"context"
	"testing"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
	"github.com/arshadm25/cto-delivery-planner/internal/syncer"
)

// Project 10 in the fake Redmine holds issues 1001-1006, all in version 100.
// 1001, 1003 and 1005 are estimated at 4h and assigned to user 8; 1003 and
// 1006 are closed.
func TestPlanIssuesInPlanner(t *testing.T) {
	e := newEnv(t)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)
	viewer := e.user("viewer@example.test", auth.Viewer, 10)

	issue := func(id int) IssueView {
		t.Helper()
		var out struct{ Issues []IssueView }
		pm.do("GET", "/api/v1/projects/10/issues?status=all&q="+itoa(id), nil, &out)
		if len(out.Issues) != 1 {
			t.Fatalf("issue %d not found", id)
		}
		return out.Issues[0]
	}

	// A new milestone in the planner gets a negative ID.
	var ms struct{ ID int }
	if st := pm.do("POST", "/api/v1/projects/10/planner-milestones", map[string]any{"name": "Phase 2", "due_date": "2026-12-01"}, &ms); st != 201 || ms.ID >= 0 {
		t.Fatalf("create milestone %d %+v", st, ms)
	}

	// Estimate, due date, assignee and milestone for two issues at once.
	body := map[string]any{"issue_ids": []int{1002, 1004}, "changes": map[string]any{
		"estimated_hours": 6, "due_date": "2026-11-20", "assignee_id": 7, "version_id": ms.ID}}
	if st := viewer.do("POST", "/api/v1/projects/10/plan", body, nil); st != 403 {
		t.Fatalf("viewer planning %d", st)
	}
	if st := pm.do("POST", "/api/v1/projects/10/plan", body, nil); st != 204 {
		t.Fatalf("plan %d", st)
	}
	is := issue(1002)
	if is.Estimated == nil || *is.Estimated != 6 || is.DueDate == nil || is.DueDate.Format("2006-01-02") != "2026-11-20" ||
		is.AssigneeID == nil || *is.AssigneeID != 7 || is.VersionID == nil || *is.VersionID != ms.ID || len(is.Planned) != 4 {
		t.Fatalf("planned issue %+v", is)
	}
	if is.VersionName == nil || *is.VersionName != "Phase 2" {
		t.Fatalf("milestone name %v", is.VersionName)
	}
	if is.RedmineValues["version_id"] == nil {
		t.Fatalf("redmine value kept: %+v", is.RedmineValues)
	}

	// The project's numbers use the planner values: every open dev issue is
	// estimated now (1001, 1005 from Redmine; 1002, 1004 from the planner).
	var p struct{ Project ProjectSummary }
	pm.do("GET", "/api/v1/projects/10", nil, &p)
	if p.Project.Issues.OpenNoEstimate != 0 {
		t.Fatalf("open without estimate %d", p.Project.Issues.OpenNoEstimate)
	}
	var mlist struct{ Milestones []MilestoneView }
	pm.do("GET", "/api/v1/projects/10/milestones", nil, &mlist)
	found := false
	for _, m := range mlist.Milestones {
		if m.ID == ms.ID {
			found = true
			if !m.Planner || m.Open != 2 || m.RedmineURL != "" {
				t.Fatalf("planner milestone %+v", m)
			}
		}
	}
	if !found {
		t.Fatal("planner milestone missing from milestones")
	}

	// A commitment can target the planner milestone.
	var c planning.Commitment
	if st := pm.do("POST", "/api/v1/commitments", map[string]any{"project_id": 10, "version_id": ms.ID, "title": "Phase 2",
		"baseline_date": "2026-12-01", "reason": "Planned in the planner"}, &c); st != 201 {
		t.Fatalf("commit to planner milestone %d", st)
	}
	if st := pm.do("DELETE", "/api/v1/projects/10/planner-milestones/"+itoa(-ms.ID), nil, nil); st != 400 {
		t.Fatalf("deleting a committed milestone %d", st)
	}

	// null goes back to Redmine's value.
	pm.do("POST", "/api/v1/projects/10/plan", map[string]any{"issue_ids": []int{1002}, "changes": map[string]any{"estimated_hours": nil}}, nil)
	if is := issue(1002); is.Estimated != nil || contains(is.Planned, "estimated_hours") {
		t.Fatalf("estimate not reverted %+v", is)
	}

	// Validation.
	for name, b := range map[string]map[string]any{
		"other project issue": {"issue_ids": []int{1007}, "changes": map[string]any{"estimated_hours": 1}},
		"unknown milestone":   {"issue_ids": []int{1002}, "changes": map[string]any{"version_id": 101}},
		"unknown assignee":    {"issue_ids": []int{1002}, "changes": map[string]any{"assignee_id": 999}},
		"bad date":            {"issue_ids": []int{1002}, "changes": map[string]any{"due_date": "20-11-2026"}},
		"unknown field":       {"issue_ids": []int{1002}, "changes": map[string]any{"subject": "x"}},
		"nothing":             {"issue_ids": []int{1002}, "changes": map[string]any{}},
	} {
		if st := pm.do("POST", "/api/v1/projects/10/plan", b, nil); st != 400 {
			t.Errorf("%s: %d", name, st)
		}
	}
	if st := pm.do("POST", "/api/v1/projects/11/plan", body, nil); st != 404 {
		t.Fatalf("pm planning another project %d", st)
	}

	// A re-sync keeps planner values; Redmine is never written to.
	if _, err := e.sy.Run(context.Background(), syncer.Incremental); err != nil {
		t.Fatal(err)
	}
	if is := issue(1004); is.AssigneeID == nil || *is.AssigneeID != 7 {
		t.Fatalf("planner value lost after sync %+v", is)
	}
	var n int
	e.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE entity IN ('issue_plans', 'planner_milestones')`).Scan(&n)
	if n != 3 {
		t.Fatalf("audit rows %d, want 3", n)
	}
}

func TestTeamsForCapacity(t *testing.T) {
	e := newEnv(t)
	pm := e.user("pm@example.test", auth.ProjectManager, 10)
	viewer := e.user("viewer@example.test", auth.Viewer, 10)
	team := map[string]any{"name": "Portal squad", "members": []map[string]any{{"person_id": 7, "hours_per_week": 30}, {"person_id": 8}}}
	if st := viewer.do("POST", "/api/v1/teams", team, nil); st != 403 {
		t.Fatalf("viewer creating team %d", st)
	}
	var created struct{ ID int64 }
	if st := pm.do("POST", "/api/v1/teams", team, &created); st != 201 {
		t.Fatalf("create team %d", st)
	}
	if st := pm.do("POST", "/api/v1/teams", team, nil); st != 400 {
		t.Fatalf("duplicate name %d", st)
	}
	if st := pm.do("POST", "/api/v1/teams", map[string]any{"name": "Ghosts", "members": []map[string]any{{"person_id": 999}}}, nil); st != 400 {
		t.Fatalf("unknown member %d", st)
	}
	var list struct{ Teams []planning.Team }
	pm.do("GET", "/api/v1/teams", nil, &list)
	if len(list.Teams) != 1 || len(list.Teams[0].Members) != 2 || list.Teams[0].Members[0].PersonName == nil {
		t.Fatalf("teams %+v", list.Teams)
	}

	alloc := map[string]any{"team_id": created.ID, "period_start": "2026-10-19", "period_end": "2026-11-15",
		"available_hours": 200, "projects": map[string]float64{"10": 160}}
	if st := pm.do("POST", "/api/v1/capacity-allocations", alloc, nil); st != 200 {
		t.Fatalf("team allocation %d", st)
	}
	var allocs struct{ Allocations []planning.Allocation }
	pm.do("GET", "/api/v1/capacity-allocations?from=2026-10-01&to=2026-12-31", nil, &allocs)
	if len(allocs.Allocations) != 1 || allocs.Allocations[0].TeamName == nil || *allocs.Allocations[0].TeamName != "Portal squad" {
		t.Fatalf("allocations %+v", allocs.Allocations)
	}
	if st := pm.do("DELETE", "/api/v1/teams/"+itoa64(created.ID), nil, nil); st != 400 {
		t.Fatalf("deleting a team in use %d", st)
	}
	two := map[string]any{"person_id": 8, "team_id": created.ID, "period_start": "2026-10-19", "period_end": "2026-11-15"}
	if st := pm.do("POST", "/api/v1/capacity-allocations", two, nil); st != 400 {
		t.Fatalf("person and team together %d", st)
	}
}
