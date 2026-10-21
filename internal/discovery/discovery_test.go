package discovery

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
)

type fakeSource struct {
	user       redmine.User
	projects   []redmine.Project
	statuses   []redmine.IssueStatus
	statusErr  error
	issues     []redmine.Issue
	issueStats redmine.PageStats
}

func (f fakeSource) CurrentUser(context.Context) (redmine.User, error) { return f.user, nil }
func (f fakeSource) ListProjects(context.Context) ([]redmine.Project, redmine.PageStats, error) {
	n := len(f.projects)
	return f.projects, redmine.PageStats{ReportedTotal: n, FinalTotal: n, Pages: 1}, nil
}
func (f fakeSource) ListIssueStatuses(context.Context) ([]redmine.IssueStatus, error) {
	return f.statuses, f.statusErr
}
func (f fakeSource) ListIssues(context.Context, url.Values) ([]redmine.Issue, redmine.PageStats, error) {
	return f.issues, f.issueStats, nil
}

func ptr[T any](v T) *T { return &v }

func sample() fakeSource {
	parent := &redmine.IDName{ID: 1, Name: "Alpha"}
	return fakeSource{
		user: redmine.User{ID: 9, Login: "svc-planner"},
		projects: []redmine.Project{
			{ID: 2, Name: "Alpha Web", Identifier: "alpha-web", Status: 1, Parent: parent},
			{ID: 1, Name: "Alpha", Identifier: "alpha", Status: 1},
			{ID: 3, Name: "Beta", Identifier: "beta", Status: 1},
		},
		statuses: []redmine.IssueStatus{{ID: 1, Name: "New"}, {ID: 5, Name: "Closed", IsClosed: true}},
		issues: []redmine.Issue{
			{ID: 10, Project: redmine.IDName{ID: 1}, Status: redmine.IDName{ID: 1},
				AssignedTo: &redmine.IDName{ID: 4, Name: "Test Person"}, EstimatedHours: ptr(3.5), DueDate: ptr("2026-11-01")},
			{ID: 11, Project: redmine.IDName{ID: 2}, Status: redmine.IDName{ID: 5}},
			{ID: 12, Project: redmine.IDName{ID: 2}, Status: redmine.IDName{ID: 1},
				Parent: &struct {
					ID int `json:"id"`
				}{ID: 99}},
			{ID: 13, Project: redmine.IDName{ID: 42}, Status: redmine.IDName{ID: 1}},
			// A duplicate from a shifted page.
			{ID: 11, Project: redmine.IDName{ID: 2}, Status: redmine.IDName{ID: 5}},
		},
		issueStats: redmine.PageStats{ReportedTotal: 4, FinalTotal: 4, Pages: 2, Duplicates: 1},
	}
}

func TestRunCountsAndHierarchy(t *testing.T) {
	r, err := Run(context.Background(), sample(), time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Issues) != 4 {
		t.Fatalf("unique issues = %d, want 4", len(r.Issues))
	}
	if r.OpenTotal != 3 || r.ClosedTotal != 1 {
		t.Fatalf("open=%d closed=%d", r.OpenTotal, r.ClosedTotal)
	}
	var order []string
	for _, pc := range r.Projects {
		order = append(order, pc.Project.Identifier)
	}
	if got := strings.Join(order, ","); got != "alpha,alpha-web,beta" {
		t.Fatalf("tree order = %s", got)
	}
	if r.Projects[1].Depth != 1 || r.Projects[1].Total != 2 || r.Projects[1].Closed != 1 {
		t.Fatalf("alpha-web row = %+v", r.Projects[1])
	}
	if r.OrphanProjectIDs[42] != 1 {
		t.Fatalf("orphan projects = %v", r.OrphanProjectIDs)
	}
	if len(r.MissingParentIDs) != 1 || r.MissingParentIDs[0] != 99 {
		t.Fatalf("missing parents = %v", r.MissingParentIDs)
	}
	if r.Gaps.NoAssignee != 3 || r.Gaps.OpenNoEstimate != 2 {
		t.Fatalf("gaps = %+v", r.Gaps)
	}
}

func TestUnknownStatusesKeepCountsUnknown(t *testing.T) {
	src := sample()
	src.statuses, src.statusErr = nil, errors.New("HTTP 403")
	r, err := Run(context.Background(), src, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var md bytes.Buffer
	if err := WriteMarkdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "Open issues: unknown") {
		t.Fatal("open count should be reported as unknown when statuses are unreadable")
	}
}

func TestOutputsContainNoPersonalNames(t *testing.T) {
	r, err := Run(context.Background(), sample(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var all bytes.Buffer
	for _, fn := range []func(*bytes.Buffer) error{
		func(b *bytes.Buffer) error { return WriteMarkdown(b, r) },
		func(b *bytes.Buffer) error { return WriteProjectsCSV(b, r) },
		func(b *bytes.Buffer) error { return WriteIssueIndexCSV(b, r) },
	} {
		if err := fn(&all); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(all.String(), "Test Person") {
		t.Fatal("assignee name leaked into the outputs")
	}
}

func TestIssueIndexLeavesMissingValuesEmpty(t *testing.T) {
	r, _ := Run(context.Background(), sample(), time.Now())
	var b bytes.Buffer
	if err := WriteIssueIndexCSV(&b, r); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	// Issue 11 has no estimate, due date or assignee.
	row := strings.Split(lines[2], ",")
	if row[0] != "11" || row[7] != "" || row[10] != "" || row[12] != "" {
		t.Fatalf("row for issue 11 = %v", row)
	}
	if !strings.Contains(lines[1], "3.5") {
		t.Fatalf("estimate missing from row for issue 10: %s", lines[1])
	}
}

func TestAdminKeyIsWarned(t *testing.T) {
	src := sample()
	src.user.Admin = true
	r, _ := Run(context.Background(), src, time.Now())
	if len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "administrator") {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}
