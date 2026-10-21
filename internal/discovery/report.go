package discovery

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
)

// Write saves report.md, projects.csv and issue-index.csv to dir.
//
// None of the files contain issue subjects, descriptions, notes or user
// names: people appear only as Redmine user IDs. Project names are included,
// so the files are internal and should not be shared outside the team.
func Write(r *Result, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	files := []struct {
		name string
		fn   func(io.Writer, *Result) error
	}{
		{"report.md", WriteMarkdown},
		{"projects.csv", WriteProjectsCSV},
		{"issue-index.csv", WriteIssueIndexCSV},
	}
	for _, f := range files {
		if err := writeFile(filepath.Join(dir, f.name), r, f.fn); err != nil {
			return fmt.Errorf("writing %s: %w", f.name, err)
		}
	}
	return nil
}

func writeFile(path string, r *Result, fn func(io.Writer, *Result) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := fn(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteMarkdown renders the human-readable baseline report.
func WriteMarkdown(w io.Writer, r *Result) error {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	count := func(n int) string {
		if !r.StatusKnown {
			return "unknown"
		}
		return strconv.Itoa(n)
	}

	p("# Redmine discovery report\n\n")
	p("Generated %s UTC by the read-only discovery tool. Only GET requests were sent.\n\n", r.GeneratedAt.Format("2006-01-02 15:04"))

	p("## Service account\n\n")
	p("- Login: `%s` (Redmine user ID %d)\n", r.User.Login, r.User.ID)
	p("- Administrator: %t\n\n", r.User.Admin)

	p("## Totals\n\n")
	p("| | API total_count | Unique fetched | Pages |\n|---|---:|---:|---:|\n")
	p("| Projects | %d | %d | %d |\n", r.ProjectStats.FinalTotal, len(r.Projects), r.ProjectStats.Pages)
	p("| Issues (open and closed) | %d | %d | %d |\n\n", r.IssueStats.FinalTotal, len(r.Issues), r.IssueStats.Pages)
	p("Open issues: %s. Closed issues: %s.\n\n", count(r.OpenTotal), count(r.ClosedTotal))

	if len(r.Warnings) > 0 {
		p("## Warnings\n\n")
		for _, w := range r.Warnings {
			p("- %s\n", w)
		}
		p("\n")
	}

	p("## Project hierarchy\n\n")
	p("Indented names are subprojects. Counts are issues assigned directly to that project, not including subprojects.\n\n")
	p("| ID | Identifier | Project | Status | Issues | Open | Closed |\n|---:|---|---|---:|---:|---:|---:|\n")
	for _, pc := range r.Projects {
		indent := strings.Repeat("&nbsp;&nbsp;&nbsp;&nbsp;", pc.Depth)
		p("| %d | `%s` | %s%s | %d | %d | %s | %s |\n", pc.Project.ID, pc.Project.Identifier, indent,
			mdEscape(pc.Project.Name), pc.Project.Status, pc.Total, count(pc.Open), count(pc.Closed))
	}
	p("\n")

	if len(r.OrphanProjectIDs) > 0 {
		p("### Issues in projects missing from the project list\n\n| Project ID | Issues |\n|---:|---:|\n")
		for _, id := range sortedKeys(boolKeys(r.OrphanProjectIDs)) {
			p("| %d | %d |\n", id, r.OrphanProjectIDs[id])
		}
		p("\n")
	}

	p("## Missing planning data\n\n")
	p("Missing values are left unknown in the planner, never treated as zero.\n\n")
	p("| Field | All issues missing it | Open issues missing it |\n|---|---:|---:|\n")
	g := r.Gaps
	p("| Assignee | %d | %s |\n", g.NoAssignee, count(g.OpenNoAssignee))
	p("| Estimated hours | %d | %s |\n", g.NoEstimate, count(g.OpenNoEstimate))
	p("| Due date | %d | %s |\n", g.NoDueDate, count(g.OpenNoDueDate))
	p("| Target version | %d | %s |\n\n", g.NoVersion, count(g.OpenNoVersion))

	if len(r.MissingParentIDs) > 0 {
		p("Parent issues referenced but not visible: %s\n\n", joinInts(r.MissingParentIDs))
	}
	if len(r.UnknownStatusIDs) > 0 {
		p("Status IDs on issues but missing from the status list: %s\n\n", joinInts(r.UnknownStatusIDs))
	}

	p("## Before accepting this baseline\n\n")
	p("- Compare the hierarchy above with Administration › Projects, including closed and archived projects.\n")
	p("- Confirm which project IDs are in scope (backlog item P1-11).\n")
	p("- Compare `issue-index.csv` with the CSV exports (backlog item P1-09).\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// WriteProjectsCSV writes one row per visible project in tree order.
func WriteProjectsCSV(w io.Writer, r *Result) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "identifier", "name", "status", "parent_id", "depth", "issues", "open", "closed"})
	for _, pc := range r.Projects {
		parent := ""
		if pc.Project.Parent != nil {
			parent = strconv.Itoa(pc.Project.Parent.ID)
		}
		open, closed := "", ""
		if r.StatusKnown {
			open, closed = strconv.Itoa(pc.Open), strconv.Itoa(pc.Closed)
		}
		cw.Write([]string{strconv.Itoa(pc.Project.ID), pc.Project.Identifier, pc.Project.Name,
			strconv.Itoa(pc.Project.Status), parent, strconv.Itoa(pc.Depth), strconv.Itoa(pc.Total), open, closed})
	}
	cw.Flush()
	return cw.Error()
}

// IssueIndexHeader is the column order of issue-index.csv.
var IssueIndexHeader = []string{"id", "project_id", "parent_id", "tracker_id", "tracker_name",
	"status_id", "status_name", "is_closed", "priority_id", "priority_name",
	"assigned_to_id", "fixed_version_id", "fixed_version_name", "start_date", "due_date",
	"done_ratio", "estimated_hours", "spent_hours", "created_on", "updated_on", "closed_on", "is_private"}

// WriteIssueIndexCSV writes one row per issue with IDs, dates, numbers and
// the names of trackers, statuses, priorities and versions. It never writes
// issue text or people's names. Empty cells mean Redmine had no value.
func WriteIssueIndexCSV(w io.Writer, r *Result) error {
	cw := csv.NewWriter(w)
	cw.Write(IssueIndexHeader)
	for _, is := range r.Issues {
		closed := ""
		if r.StatusKnown {
			closed = strconv.FormatBool(r.IsClosed(is))
		}
		var parent string
		if is.Parent != nil {
			parent = strconv.Itoa(is.Parent.ID)
		}
		cw.Write([]string{
			strconv.Itoa(is.ID), strconv.Itoa(is.Project.ID), parent,
			strconv.Itoa(is.Tracker.ID), is.Tracker.Name,
			strconv.Itoa(is.Status.ID), is.Status.Name, closed,
			strconv.Itoa(is.Priority.ID), is.Priority.Name,
			refID(is.AssignedTo), refID(is.FixedVersion), refName(is.FixedVersion),
			str(is.StartDate), str(is.DueDate), intStr(is.DoneRatio),
			floatStr(is.EstimatedHours), floatStr(is.SpentHours),
			is.CreatedOn, is.UpdatedOn, str(is.ClosedOn), boolStr(is.IsPrivate),
		})
	}
	cw.Flush()
	return cw.Error()
}

func refID(r *redmine.IDName) string {
	if r == nil {
		return ""
	}
	return strconv.Itoa(r.ID)
}

func refName(r *redmine.IDName) string {
	if r == nil {
		return ""
	}
	return r.Name
}

func boolStr(b *bool) string {
	if b == nil {
		return ""
	}
	return strconv.FormatBool(*b)
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intStr(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

func floatStr(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

func boolKeys(m map[int]int) map[int]bool {
	out := make(map[int]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func mdEscape(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}
