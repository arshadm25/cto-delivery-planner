package csvcompare

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Fixtures are fictional. Column order and formats follow a Redmine
// English-language CSV export with DD-MM-YYYY dates and 12-hour times.
const exportA = `#,Project,Tracker,Parent task,Parent task subject,Status,Priority,Subject,Author,Assignee,Updated,Category,Target version,Start date,Due date,Estimated time,Total estimated time,Spent time,Total spent time,% Done,Created,Closed,Last updated by,Related issues,Files,Story Points,Private,Description,Last notes
101,Demo Web,Bug,"","",New,Normal,Login button misaligned,Test Author,Test Person,19-10-2026 05:45 PM,"",Sprint 9,19-10-2026,"","","",0.00,0.00,0,19-10-2026 05:45 PM,"","","","","",No,"password: hunter2-not-real",""
102,Demo Web,Feature,101,Login button misaligned,In Progress,High,Add export,Test Author,"",16-10-2026 10:00 AM,"","",16-10-2026,23-10-2026,4.00,4.00,1.50,1.50,50,16-10-2026 09:00 AM,"","","","","",No,"multi
line
description",""
103,Demo Web,Task,"","",New,Normal,Gone from API,Test Author,"",15-10-2026 10:00 AM,"","",15-10-2026,"","","",0.00,0.00,0,15-10-2026 09:00 AM,"","","","","",Yes,"",""
104,Demo App,Task,"","",New,Normal,Edited later,Test Author,"",14-10-2026 10:00 AM,"","",14-10-2026,"","","",0.00,0.00,0,14-10-2026 09:00 AM,"","","","","",No,"",""
`

const projectsCSV = `id,identifier,name,status,parent_id,depth,issues,open,closed
1,demo-web,Demo Web,1,,0,4,3,1
2,demo-app,Demo App,1,,0,1,1,0
3,other,Other Project,1,,0,1,1,0
`

const indexCSV = `id,project_id,parent_id,tracker_id,tracker_name,status_id,status_name,is_closed,priority_id,priority_name,assigned_to_id,fixed_version_id,fixed_version_name,start_date,due_date,done_ratio,estimated_hours,spent_hours,created_on,updated_on,closed_on,is_private
101,1,,1,Bug,1,New,false,2,Normal,7,5,Sprint 9,2026-10-19,,0,,0,2026-10-19T12:15:00Z,2026-10-19T12:15:30Z,,false
102,1,101,2,Feature,2,In Progress,false,3,High,,,,2026-10-16,2026-10-23,40,4,1.5,2026-10-16T03:30:00Z,2026-10-16T04:30:00Z,,false
104,2,,3,Task,1,New,false,2,Normal,,,,2026-10-14,,0,,0,2026-10-14T03:30:00Z,2026-10-20T08:00:00Z,,false
105,1,,3,Task,5,Closed,true,2,Normal,,,,2026-09-01,,100,,0,2026-09-01T03:30:00Z,2026-09-02T03:30:00Z,2026-09-02T03:30:00Z,false
106,3,,3,Task,1,New,false,2,Normal,,,,2026-09-01,,0,,0,2026-09-01T03:30:00Z,2026-09-02T03:30:00Z,,false
`

func layoutIST(t *testing.T) Layout {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	return Layout{Date: "02-01-2006", DateTime: "02-01-2006 03:04 PM", Location: loc}
}

func run(t *testing.T) *Result {
	t.Helper()
	ex, err := ParseExport("a.csv", strings.NewReader(exportA), layoutIST(t))
	if err != nil {
		t.Fatal(err)
	}
	projects, err := LoadProjects(strings.NewReader(projectsCSV))
	if err != nil {
		t.Fatal(err)
	}
	index, err := LoadIndex(strings.NewReader(indexCSV))
	if err != nil {
		t.Fatal(err)
	}
	return Compare([]*Export{ex}, projects, index)
}

func find(r *Result, id int) *IssueDiff {
	for i := range r.Diffs {
		if r.Diffs[i].ID == id {
			return &r.Diffs[i]
		}
	}
	return nil
}

func TestParseExportKeepsBlanksAsNil(t *testing.T) {
	ex, err := ParseExport("a.csv", strings.NewReader(exportA), layoutIST(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Issues) != 4 {
		t.Fatalf("rows = %d, want 4", len(ex.Issues))
	}
	first := ex.Issues[0]
	if first.Estimated != nil || first.DueDate != nil || first.Parent != nil {
		t.Fatal("blank cells should parse as nil")
	}
	if !first.HasAssignee || *first.StartDate != "2026-10-19" {
		t.Fatalf("first row = %+v", first)
	}
	// 05:45 PM IST is 12:15 UTC.
	if got := first.Updated.UTC().Format(time.RFC3339); got != "2026-10-19T12:15:00Z" {
		t.Fatalf("updated = %s", got)
	}
}

func TestParseExportRejectsTranslatedHeaders(t *testing.T) {
	_, err := ParseExport("x.csv", strings.NewReader("#,Projet,Statut\n1,a,b\n"), layoutIST(t))
	if err == nil || !strings.Contains(err.Error(), "English") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompareClassifies(t *testing.T) {
	r := run(t)

	if d := find(r, 101); d != nil {
		t.Fatalf("issue 101 should match exactly, got %+v", d)
	}
	if d := find(r, 102); d == nil || d.Category != Mismatch || len(d.Fields) != 1 || d.Fields[0].Field != "done_ratio" {
		t.Fatalf("issue 102 = %+v", d)
	}
	if d := find(r, 103); d == nil || d.Category != MissingFromAPI {
		t.Fatalf("issue 103 = %+v", d)
	}
	if d := find(r, 104); d != nil {
		t.Fatalf("issue 104 has no field differences, got %+v", d)
	}
	if d := find(r, 105); d == nil || d.Category != MissingFromCSV || !strings.Contains(d.Reason, "closed") {
		t.Fatalf("issue 105 = %+v", d)
	}
	if find(r, 106) != nil || r.OutOfScopeAPI[3] != 1 {
		t.Fatalf("issue 106 is in a project with no CSV rows and should only be counted out of scope")
	}
	if r.Matched != 3 || r.Identical != 2 || r.APIInScope != 4 {
		t.Fatalf("matched=%d identical=%d inScope=%d", r.Matched, r.Identical, r.APIInScope)
	}
}

func TestChangedAfterExport(t *testing.T) {
	ex, _ := ParseExport("a.csv", strings.NewReader(exportA), layoutIST(t))
	projects, _ := LoadProjects(strings.NewReader(projectsCSV))
	index, _ := LoadIndex(strings.NewReader(indexCSV))
	// Issue 104 was edited in the API after the export; give it a new status.
	for i := range index {
		if index[i].ID == 104 {
			index[i].Status = "In Progress"
		}
	}
	r := Compare([]*Export{ex}, projects, index)
	if d := find(r, 104); d == nil || d.Category != ChangedAfterExport {
		t.Fatalf("issue 104 = %+v", d)
	}
}

func TestDuplicatesAcrossFiles(t *testing.T) {
	a, _ := ParseExport("a.csv", strings.NewReader(exportA), layoutIST(t))
	b, _ := ParseExport("b.csv", strings.NewReader(exportA), layoutIST(t))
	projects, _ := LoadProjects(strings.NewReader(projectsCSV))
	index, _ := LoadIndex(strings.NewReader(indexCSV))
	r := Compare([]*Export{a, b}, projects, index)
	if r.Count(DuplicateInCSV) != 4 || r.CSVUnique != 4 {
		t.Fatalf("duplicates=%d unique=%d", r.Count(DuplicateInCSV), r.CSVUnique)
	}
}

func TestOutputsNeverContainFreeTextOrNames(t *testing.T) {
	r := run(t)
	var all bytes.Buffer
	if err := WriteMarkdown(&all, r); err != nil {
		t.Fatal(err)
	}
	if err := WriteDetailsCSV(&all, r); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"hunter2", "Test Person", "Test Author", "Login button", "multi"} {
		if strings.Contains(all.String(), s) {
			t.Errorf("output contains %q", s)
		}
	}
}
