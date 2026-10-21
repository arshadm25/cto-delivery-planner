package csvcompare

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// Category is how a CSV-versus-API difference is classified.
type Category string

const (
	// Field values differ and the API copy was updated after the CSV row,
	// so the difference is most likely an edit made after the export.
	ChangedAfterExport Category = "changed_after_export"
	// Field values differ but the API copy is not newer than the CSV row.
	Mismatch Category = "mismatch"
	// In a CSV export but not returned by the API: the service account
	// cannot see it, or it was deleted or moved out of view.
	MissingFromAPI Category = "missing_from_api"
	// Returned by the API in a project covered by the exports, but not in
	// any CSV.
	MissingFromCSV Category = "missing_from_csv"
	// The same issue ID appears in more than one CSV row.
	DuplicateInCSV Category = "duplicate_in_csv"
)

// ExportLimitHint is Redmine's default CSV export row limit. A file with
// exactly this many rows has probably been cut off.
const ExportLimitHint = 500

// FieldDiff is one differing field on one issue.
type FieldDiff struct {
	Field    string
	CSVValue string
	APIValue string
}

// IssueDiff is one classified issue.
type IssueDiff struct {
	ID       int
	File     string // CSV file, empty for MissingFromCSV
	Category Category
	Reason   string
	Fields   []FieldDiff
}

// ProjectMatch links a CSV project label to an API project.
type ProjectMatch struct {
	Label     string
	CSVIssues int
	APIID     int // 0 when unmatched
	APIIssues int
}

// FileSummary describes one export file.
type FileSummary struct {
	File           string
	Rows           int
	ClosedRows     int
	LikelyTruncate bool
	MissingCols    []string
}

// Result is the full comparison.
type Result struct {
	Files         []FileSummary
	Projects      []ProjectMatch
	CSVUnique     int
	APIInScope    int
	Matched       int
	Identical     int
	Diffs         []IssueDiff
	FieldCounts   map[string]int
	OutOfScopeAPI map[int]int // project ID → API issues in projects not in any CSV
	NewestCSVTime time.Time
}

// Count returns how many issues fall in a category.
func (r *Result) Count(c Category) int {
	n := 0
	for _, d := range r.Diffs {
		if d.Category == c {
			n++
		}
	}
	return n
}

// Compare classifies every issue in the exports and in the API index.
// Projects are matched by name first, then by identifier.
func Compare(exports []*Export, projects []Project, api []APIIssue) *Result {
	r := &Result{FieldCounts: map[string]int{}, OutOfScopeAPI: map[int]int{}}

	byName := map[string]Project{}
	for _, p := range projects {
		byName[p.Name] = p
	}
	for _, p := range projects {
		if _, taken := byName[p.Identifier]; !taken {
			byName[p.Identifier] = p
		}
	}
	projName := map[int]string{}
	for _, p := range projects {
		projName[p.ID] = p.Name
	}
	apiByID := map[int]APIIssue{}
	apiPerProject := map[int]int{}
	for _, is := range api {
		apiByID[is.ID] = is
		apiPerProject[is.ProjectID]++
	}

	labelCounts := map[string]int{}
	seen := map[int]CSVIssue{}
	for _, ex := range exports {
		fs := FileSummary{File: ex.File, Rows: len(ex.Issues), MissingCols: ex.MissingCols}
		fs.LikelyTruncate = len(ex.Issues) == ExportLimitHint
		for _, is := range ex.Issues {
			labelCounts[is.Project]++
			if is.Closed != nil {
				fs.ClosedRows++
			}
			if is.Updated.After(r.NewestCSVTime) {
				r.NewestCSVTime = is.Updated
			}
			if prev, dup := seen[is.ID]; dup {
				r.Diffs = append(r.Diffs, IssueDiff{ID: is.ID, File: is.File, Category: DuplicateInCSV,
					Reason: fmt.Sprintf("also in %s row %d", prev.File, prev.Row)})
				continue
			}
			seen[is.ID] = is
		}
		r.Files = append(r.Files, fs)
	}
	r.CSVUnique = len(seen)

	inScope := map[int]bool{}
	for label, n := range labelCounts {
		pm := ProjectMatch{Label: label, CSVIssues: n}
		if p, ok := byName[label]; ok {
			pm.APIID, pm.APIIssues = p.ID, apiPerProject[p.ID]
			inScope[p.ID] = true
		}
		r.Projects = append(r.Projects, pm)
	}
	sort.Slice(r.Projects, func(i, j int) bool { return r.Projects[i].Label < r.Projects[j].Label })

	ids := make([]int, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		c := seen[id]
		a, ok := apiByID[id]
		if !ok {
			r.Diffs = append(r.Diffs, IssueDiff{ID: id, File: c.File, Category: MissingFromAPI,
				Reason: "not returned by the API; check the service account's project membership and private-issue permission"})
			continue
		}
		r.Matched++
		fields := diffFields(c, a, byName, projName)
		if len(fields) == 0 {
			r.Identical++
			continue
		}
		cat, reason := Mismatch, "API copy is not newer than the CSV row"
		// CSV timestamps have minute precision, so allow for the dropped seconds.
		if a.Updated.After(c.Updated.Add(time.Minute)) {
			cat, reason = ChangedAfterExport, "API updated_on is later than the CSV Updated value"
		}
		for _, f := range fields {
			r.FieldCounts[f.Field]++
		}
		r.Diffs = append(r.Diffs, IssueDiff{ID: id, File: c.File, Category: cat, Reason: reason, Fields: fields})
	}

	for _, a := range api {
		if !inScope[a.ProjectID] {
			r.OutOfScopeAPI[a.ProjectID]++
			continue
		}
		r.APIInScope++
		if _, ok := seen[a.ID]; ok {
			continue
		}
		reason := "not in any export"
		switch {
		case a.Closed != nil && *a.Closed:
			reason = "closed; exports filtered to open issues leave these out"
		case a.Updated.After(r.NewestCSVTime.Add(time.Minute)):
			reason = "updated after the newest CSV row; may have been created after the export"
		}
		r.Diffs = append(r.Diffs, IssueDiff{ID: a.ID, Category: MissingFromCSV, Reason: reason})
	}
	sort.SliceStable(r.Diffs, func(i, j int) bool {
		if r.Diffs[i].Category != r.Diffs[j].Category {
			return r.Diffs[i].Category < r.Diffs[j].Category
		}
		return r.Diffs[i].ID < r.Diffs[j].ID
	})
	return r
}

func diffFields(c CSVIssue, a APIIssue, byName map[string]Project, projName map[int]string) []FieldDiff {
	var out []FieldDiff
	add := func(field, cv, av string) {
		if cv != av {
			out = append(out, FieldDiff{field, cv, av})
		}
	}
	if p, ok := byName[c.Project]; !ok || p.ID != a.ProjectID {
		out = append(out, FieldDiff{"project", c.Project, projName[a.ProjectID]})
	}
	add("tracker", c.Tracker, a.Tracker)
	add("status", c.Status, a.Status)
	add("priority", c.Priority, a.Priority)
	add("assignee", setOrEmpty(c.HasAssignee), setOrEmpty(a.HasAssignee))
	add("parent", intOrBlank(c.Parent), intOrBlank(a.Parent))
	add("target_version", strOrBlank(c.Version), strOrBlank(a.Version))
	add("start_date", strOrBlank(c.StartDate), strOrBlank(a.StartDate))
	add("due_date", strOrBlank(c.DueDate), strOrBlank(a.DueDate))
	add("done_ratio", intOrBlank(c.DoneRatio), intOrBlank(a.DoneRatio))
	if !sameHours(c.Estimated, a.Estimated) {
		out = append(out, FieldDiff{"estimated_hours", floatOrBlank(c.Estimated), floatOrBlank(a.Estimated)})
	}
	// Redmine shows 0.00 spent time when nothing is logged, and some
	// versions leave spent_hours out of issue listings, so compare only
	// when both sides have a value.
	if c.Spent != nil && a.Spent != nil && !sameHours(c.Spent, a.Spent) {
		out = append(out, FieldDiff{"spent_hours", floatOrBlank(c.Spent), floatOrBlank(a.Spent)})
	}
	add("closed", setOrEmpty(c.Closed != nil), setOrEmpty(a.ClosedOn != nil))
	if c.Private != nil && a.Private != nil {
		add("private", strconv.FormatBool(*c.Private), strconv.FormatBool(*a.Private))
	}
	return out
}

func sameHours(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return math.Abs(*a-*b) < 0.005
}

func setOrEmpty(b bool) string {
	if b {
		return "set"
	}
	return "empty"
}

func intOrBlank(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

func strOrBlank(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func floatOrBlank(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', 2, 64)
}
