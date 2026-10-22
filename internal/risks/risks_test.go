package risks

import (
	"testing"
	"time"
)

var today = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)

func d(s string) *time.Time { t, _ := time.Parse(time.DateOnly, s); return &t }

func kinds(fs []Flag, issue int) map[string]Severity {
	out := map[string]Severity{}
	for _, f := range fs {
		if f.IssueID != nil && *f.IssueID == issue {
			out[f.Kind] = f.Severity
		}
	}
	return out
}

func TestIssueFlags(t *testing.T) {
	est, spent := 2.0, 5.0
	who := 3
	issues := []Issue{
		{ID: 1, ProjectID: 9, PriorityName: "Normal", UpdatedOn: today}, // blocker, missing fields
		{ID: 2, ProjectID: 9, PriorityName: "High", AssigneeID: &who, Estimated: &est, Spent: &spent, DueDate: d("2026-10-01"), UpdatedOn: today.AddDate(0, 0, -20)},
		{ID: 3, ProjectID: 9, IsClosed: true, UpdatedOn: today},
		{ID: 4, ProjectID: 9, PriorityName: "Normal", AssigneeID: &who, Estimated: &est, DueDate: d("2026-11-01"), UpdatedOn: today},
	}
	rels := []Relation{{From: 1, To: 2, Type: "blocks"}, {From: 3, To: 4, Type: "blocks"}, {From: 1, To: 4, Type: "relates"}}
	fs := IssueFlags(issues, rels, Rules{Today: today})

	got := kinds(fs, 2)
	want := map[string]Severity{KindBlocked: Critical, KindOverdue: High, KindStale: Medium, KindOverEstimate: Low}
	for k, s := range want {
		if got[k] != s {
			t.Errorf("issue 2 %s = %q, want %q (all %v)", k, got[k], s, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("issue 2 flags %v", got)
	}
	g1 := kinds(fs, 1)
	for _, k := range []string{KindMissingAssignee, KindMissingEstimate, KindMissingDueDate} {
		if _, ok := g1[k]; !ok {
			t.Errorf("issue 1 missing %s", k)
		}
	}
	if len(kinds(fs, 3)) != 0 {
		t.Error("closed issue flagged")
	}
	if len(kinds(fs, 4)) != 0 {
		t.Errorf("issue 4: closed blocker and 'relates' must not flag: %v", kinds(fs, 4))
	}
}

func TestMilestoneFlags(t *testing.T) {
	v := func(id int) *int { return &id }
	versions := []Version{
		{ID: 1, ProjectID: 9, Name: "Sprint 4", Status: "open", Due: d("2026-10-10")},
		{ID: 2, ProjectID: 9, Name: "UAT sign-off", Status: "open", Due: d("2026-10-30")},
		{ID: 3, ProjectID: 9, Name: "Backlog", Status: "open"},
		{ID: 4, ProjectID: 9, Name: "Sprint 6", Status: "open", Due: d("2026-10-29")},
		{ID: 5, ProjectID: 9, Name: "Done", Status: "closed", Due: d("2026-01-01")},
	}
	issues := []Issue{
		{ID: 1, VersionID: v(1)}, {ID: 2, VersionID: v(2)}, {ID: 3, VersionID: v(3)},
		{ID: 4, VersionID: v(4)}, {ID: 5, VersionID: v(4), IsClosed: true}, {ID: 6, VersionID: v(4), IsClosed: true},
		{ID: 7, VersionID: v(5)},
	}
	fs := MilestoneFlags(versions, issues, Rules{Today: today})
	got := map[int]string{}
	for _, f := range fs {
		got[*f.VersionID] = f.Kind
	}
	want := map[int]string{1: KindMilestoneOverdue, 2: KindReleaseReadiness, 3: KindMilestoneNoDate}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for id, k := range want {
		if got[id] != k {
			t.Errorf("version %d = %s, want %s", id, got[id], k)
		}
	}
}

func TestProjectHealth(t *testing.T) {
	cases := []struct {
		open     int
		schedule bool
		flags    []Flag
		want     State
	}{
		{5, true, []Flag{{Kind: KindBlocked, Severity: Critical}}, Blocked},
		{5, true, []Flag{{Kind: KindOverdue, Severity: High}, {Kind: KindStale, Severity: Medium}}, AtRisk},
		{5, true, []Flag{{Kind: KindMissingEstimate, Severity: Low}}, OnTrack},
		{5, false, nil, InsufficientData},
		{0, false, nil, InsufficientData},
	}
	for i, c := range cases {
		if h := ProjectHealth(c.open, c.schedule, c.flags); h.State != c.want {
			t.Errorf("case %d: %s, want %s", i, h.State, c.want)
		}
	}
}

func TestAwaitingQASkipsDevelopmentFlags(t *testing.T) {
	is := Issue{ID: 7, ProjectID: 9, StatusName: "Fixed Not Tested", PriorityName: "Normal",
		DueDate: d("2026-10-01"), UpdatedOn: today.AddDate(0, 0, -30)}
	got := kinds(IssueFlags([]Issue{is}, nil, Rules{Today: today}), 7)
	for _, k := range []string{KindOverdue, KindMissingAssignee} {
		if _, ok := got[k]; !ok {
			t.Errorf("awaiting QA issue should keep %s: %v", k, got)
		}
	}
	for _, k := range []string{KindStale, KindMissingEstimate} {
		if _, ok := got[k]; ok {
			t.Errorf("awaiting QA issue should not get %s", k)
		}
	}
	// An explicit empty list turns the bucket off.
	off := kinds(IssueFlags([]Issue{is}, nil, Rules{Today: today, AwaitingQAStatuses: []string{}}), 7)
	if _, ok := off[KindStale]; !ok {
		t.Errorf("with no awaiting QA statuses the issue is stale: %v", off)
	}
	if (Rules{}).AwaitingQA(Issue{StatusName: "fixed not tested", IsClosed: true}) {
		t.Error("closed issue reported as awaiting QA")
	}
}
