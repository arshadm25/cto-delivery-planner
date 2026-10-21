// Package csvcompare checks Redmine CSV issue exports against the issue index
// written by the discovery tool, issue by issue.
//
// Only structural columns are read from the exports. Subject, description,
// notes, author and other free-text columns are never parsed or written out,
// and assignees are compared only as "set" or "empty".
package csvcompare

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Column names in a Redmine CSV export with the interface language set to
// English. Exports in other languages use translated headers and are refused.
const (
	colID        = "#"
	colProject   = "Project"
	colTracker   = "Tracker"
	colParent    = "Parent task"
	colStatus    = "Status"
	colPriority  = "Priority"
	colAssignee  = "Assignee"
	colUpdated   = "Updated"
	colVersion   = "Target version"
	colStartDate = "Start date"
	colDueDate   = "Due date"
	colEstimated = "Estimated time"
	colSpent     = "Spent time"
	colDone      = "% Done"
	colCreated   = "Created"
	colClosed    = "Closed"
	colPrivate   = "Private"
)

// requiredColumns must all be present in an export.
var requiredColumns = []string{colID, colProject, colTracker, colStatus, colPriority,
	colAssignee, colUpdated, colStartDate, colDueDate, colEstimated, colDone, colCreated}

// optionalColumns are compared when present.
var optionalColumns = []string{colParent, colVersion, colSpent, colClosed, colPrivate}

// Layout describes how dates are written in the export, which follows the
// exporting user's Redmine preferences.
type Layout struct {
	Date     string         // Go layout for dates, e.g. "02-01-2006"
	DateTime string         // Go layout for timestamps, e.g. "02-01-2006 03:04 PM"
	Location *time.Location // time zone of the exporting user
}

// CSVIssue is one export row reduced to comparable fields. Nil means the
// cell was blank.
type CSVIssue struct {
	File        string
	Row         int
	ID          int
	Project     string
	Tracker     string
	Parent      *int
	Status      string
	Priority    string
	HasAssignee bool
	Version     *string
	StartDate   *string // normalised to YYYY-MM-DD
	DueDate     *string // normalised to YYYY-MM-DD
	Estimated   *float64
	Spent       *float64
	DoneRatio   *int
	Created     time.Time
	Updated     time.Time
	Closed      *time.Time
	Private     *bool
}

// Export is one parsed CSV file.
type Export struct {
	File        string
	Issues      []CSVIssue
	Columns     []string
	MissingCols []string // optional columns not present
}

// ParseExport reads a Redmine CSV issue export.
func ParseExport(name string, r io.Reader, l Layout) (*Export, error) {
	cr := csv.NewReader(r)
	cr.ReuseRecord = false
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: reading header: %w", name, err)
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	var missing []string
	for _, c := range requiredColumns {
		if _, ok := idx[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s: missing columns %q; export with the Redmine interface language set to English", name, missing)
	}
	ex := &Export{File: name, Columns: header}
	for _, c := range optionalColumns {
		if _, ok := idx[c]; !ok {
			ex.MissingCols = append(ex.MissingCols, c)
		}
	}

	row := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		row++
		if err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", name, row, err)
		}
		cell := func(c string) string {
			if i, ok := idx[c]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		is, err := parseRow(cell, l)
		if err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", name, row, err)
		}
		is.File, is.Row = name, row
		ex.Issues = append(ex.Issues, is)
	}
	return ex, nil
}

func parseRow(cell func(string) string, l Layout) (CSVIssue, error) {
	var is CSVIssue
	var err error
	if is.ID, err = strconv.Atoi(cell(colID)); err != nil {
		return is, fmt.Errorf("issue ID %q is not a number", cell(colID))
	}
	is.Project = cell(colProject)
	is.Tracker = cell(colTracker)
	is.Status = cell(colStatus)
	is.Priority = cell(colPriority)
	is.HasAssignee = cell(colAssignee) != ""
	if v := cell(colVersion); v != "" {
		is.Version = &v
	}
	if is.Parent, err = optInt(cell(colParent)); err != nil {
		return is, fmt.Errorf("issue %d parent: %w", is.ID, err)
	}
	if is.DoneRatio, err = optInt(cell(colDone)); err != nil {
		return is, fmt.Errorf("issue %d %% done: %w", is.ID, err)
	}
	if is.Estimated, err = optFloat(cell(colEstimated)); err != nil {
		return is, fmt.Errorf("issue %d estimate: %w", is.ID, err)
	}
	if is.Spent, err = optFloat(cell(colSpent)); err != nil {
		return is, fmt.Errorf("issue %d spent time: %w", is.ID, err)
	}
	if is.StartDate, err = optDate(cell(colStartDate), l); err != nil {
		return is, fmt.Errorf("issue %d start date: %w", is.ID, err)
	}
	if is.DueDate, err = optDate(cell(colDueDate), l); err != nil {
		return is, fmt.Errorf("issue %d due date: %w", is.ID, err)
	}
	if is.Created, err = time.ParseInLocation(l.DateTime, cell(colCreated), l.Location); err != nil {
		return is, fmt.Errorf("issue %d created: %w", is.ID, err)
	}
	if is.Updated, err = time.ParseInLocation(l.DateTime, cell(colUpdated), l.Location); err != nil {
		return is, fmt.Errorf("issue %d updated: %w", is.ID, err)
	}
	if v := cell(colClosed); v != "" {
		t, err := time.ParseInLocation(l.DateTime, v, l.Location)
		if err != nil {
			return is, fmt.Errorf("issue %d closed: %w", is.ID, err)
		}
		is.Closed = &t
	}
	switch cell(colPrivate) {
	case "Yes":
		b := true
		is.Private = &b
	case "No":
		b := false
		is.Private = &b
	}
	return is, nil
}

func optInt(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not a whole number", s)
	}
	return &n, nil
}

func optFloat(s string) (*float64, error) {
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a number", s)
	}
	return &f, nil
}

func optDate(s string, l Layout) (*string, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(l.Date, s)
	if err != nil {
		return nil, err
	}
	d := t.Format("2006-01-02")
	return &d, nil
}
