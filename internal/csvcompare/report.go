package csvcompare

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// maxListed caps how many issue IDs the Markdown report lists per category.
// The details CSV always lists every issue.
const maxListed = 100

// WriteMarkdown renders the comparison summary.
func WriteMarkdown(w io.Writer, r *Result) error {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	p("# CSV export vs Redmine API comparison\n\n")
	p("Compares the CSV exports with the discovery tool's issue index by issue ID. Differences are classified, not treated as errors: the API reflects edits made after the export.\n\n")

	p("## Export files\n\n| File | Rows | Rows with a Closed date | Notes |\n|---|---:|---:|---|\n")
	for _, f := range r.Files {
		var notes []string
		if f.LikelyTruncate {
			notes = append(notes, fmt.Sprintf("exactly %d rows: probably cut off by the export limit", ExportLimitHint))
		}
		if len(f.MissingCols) > 0 {
			notes = append(notes, "no columns: "+strings.Join(f.MissingCols, ", "))
		}
		p("| %s | %d | %d | %s |\n", mdCell(f.File), f.Rows, f.ClosedRows, strings.Join(notes, "; "))
	}
	p("\n")

	p("## Summary\n\n| | Issues |\n|---|---:|\n")
	p("| Unique issue IDs in the exports | %d |\n", r.CSVUnique)
	p("| API issues in the same projects | %d |\n", r.APIInScope)
	p("| Matched by ID | %d |\n", r.Matched)
	p("| Matched with no differences | %d |\n", r.Identical)
	for _, c := range []Category{ChangedAfterExport, Mismatch, MissingFromAPI, MissingFromCSV, DuplicateInCSV} {
		p("| %s | %d |\n", label(c), r.Count(c))
	}
	p("\n")

	p("## Project labels\n\n| CSV label | CSV issues | API project ID | API issues |\n|---|---:|---:|---:|\n")
	for _, pm := range r.Projects {
		id, n := "not found", "–"
		if pm.APIID != 0 {
			id, n = strconv.Itoa(pm.APIID), strconv.Itoa(pm.APIIssues)
		}
		p("| %s | %d | %s | %s |\n", mdCell(pm.Label), pm.CSVIssues, id, n)
	}
	p("\n")
	if len(r.OutOfScopeAPI) > 0 {
		p("API projects with issues but no CSV rows (project ID: issues): ")
		var parts []string
		for _, id := range sortedIntKeys(r.OutOfScopeAPI) {
			parts = append(parts, fmt.Sprintf("%d: %d", id, r.OutOfScopeAPI[id]))
		}
		p("%s\n\n", strings.Join(parts, ", "))
	}

	if len(r.FieldCounts) > 0 {
		p("## Fields that differ on matched issues\n\n| Field | Issues |\n|---|---:|\n")
		fields := make([]string, 0, len(r.FieldCounts))
		for f := range r.FieldCounts {
			fields = append(fields, f)
		}
		sort.Slice(fields, func(i, j int) bool {
			return r.FieldCounts[fields[i]] > r.FieldCounts[fields[j]] || (r.FieldCounts[fields[i]] == r.FieldCounts[fields[j]] && fields[i] < fields[j])
		})
		for _, f := range fields {
			p("| %s | %d |\n", f, r.FieldCounts[f])
		}
		p("\n")
	}

	for _, c := range []Category{MissingFromAPI, Mismatch, MissingFromCSV, DuplicateInCSV} {
		var ds []IssueDiff
		reasons := map[string]int{}
		for _, d := range r.Diffs {
			if d.Category == c {
				ds = append(ds, d)
				reasons[d.Reason]++
			}
		}
		if len(ds) == 0 {
			continue
		}
		p("## %s (%d)\n\n", label(c), len(ds))
		for _, reason := range sortedStrKeys(reasons) {
			p("- %d: %s\n", reasons[reason], reason)
		}
		p("\nIssue IDs: %s\n\n", idList(ds))
	}
	p("Full detail per issue and field is in `compare-details.csv`.\n")

	_, err := io.WriteString(w, b.String())
	return err
}

// WriteDetailsCSV writes one row per differing field, or one row per issue
// for categories without field detail.
func WriteDetailsCSV(w io.Writer, r *Result) error {
	cw := csv.NewWriter(w)
	cw.Write([]string{"issue_id", "csv_file", "category", "reason", "field", "csv_value", "api_value"})
	for _, d := range r.Diffs {
		base := []string{strconv.Itoa(d.ID), d.File, string(d.Category), d.Reason}
		if len(d.Fields) == 0 {
			cw.Write(append(base, "", "", ""))
			continue
		}
		for _, f := range d.Fields {
			cw.Write(append(append([]string{}, base...), f.Field, f.CSVValue, f.APIValue))
		}
	}
	cw.Flush()
	return cw.Error()
}

func label(c Category) string {
	switch c {
	case ChangedAfterExport:
		return "Differs, changed after export"
	case Mismatch:
		return "Differs, not explained by a later edit"
	case MissingFromAPI:
		return "In CSV, missing from API"
	case MissingFromCSV:
		return "In API, missing from CSV"
	case DuplicateInCSV:
		return "Duplicate ID in CSV"
	}
	return string(c)
}

func idList(ds []IssueDiff) string {
	n := len(ds)
	if n > maxListed {
		ds = ds[:maxListed]
	}
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = strconv.Itoa(d.ID)
	}
	s := strings.Join(parts, ", ")
	if n > maxListed {
		s += fmt.Sprintf(" and %d more", n-maxListed)
	}
	return s
}

func mdCell(s string) string { return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s) }

func sortedIntKeys(m map[int]int) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func sortedStrKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
