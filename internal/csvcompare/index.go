package csvcompare

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"
)

// APIIssue is one row of the discovery tool's issue-index.csv.
type APIIssue struct {
	ID          int
	ProjectID   int
	Parent      *int
	Tracker     string
	Status      string
	Closed      *bool
	Priority    string
	HasAssignee bool
	Version     *string
	StartDate   *string
	DueDate     *string
	DoneRatio   *int
	Estimated   *float64
	Spent       *float64
	Updated     time.Time
	ClosedOn    *time.Time
	Private     *bool
}

// Project is one row of the discovery tool's projects.csv.
type Project struct {
	ID         int
	Identifier string
	Name       string
}

// readTable reads a CSV with a header row into maps keyed by column name.
func readTable(name string, r io.Reader, required []string) ([]map[string]string, error) {
	cr := csv.NewReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: reading header: %w", name, err)
	}
	have := map[string]bool{}
	for _, h := range header {
		have[h] = true
	}
	for _, c := range required {
		if !have[c] {
			return nil, fmt.Errorf("%s: missing column %q; re-run the discovery tool to regenerate it", name, c)
		}
	}
	var out []map[string]string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		m := make(map[string]string, len(header))
		for i, h := range header {
			if i < len(rec) {
				m[h] = rec[i]
			}
		}
		out = append(out, m)
	}
}

// LoadProjects reads projects.csv.
func LoadProjects(r io.Reader) ([]Project, error) {
	rows, err := readTable("projects.csv", r, []string{"id", "identifier", "name"})
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(rows))
	for _, m := range rows {
		id, err := strconv.Atoi(m["id"])
		if err != nil {
			return nil, fmt.Errorf("projects.csv: bad id %q", m["id"])
		}
		out = append(out, Project{ID: id, Identifier: m["identifier"], Name: m["name"]})
	}
	return out, nil
}

// LoadIndex reads issue-index.csv.
func LoadIndex(r io.Reader) ([]APIIssue, error) {
	rows, err := readTable("issue-index.csv", r, []string{"id", "project_id", "parent_id",
		"tracker_name", "status_name", "is_closed", "priority_name", "assigned_to_id",
		"fixed_version_name", "start_date", "due_date", "done_ratio", "estimated_hours",
		"spent_hours", "updated_on", "closed_on", "is_private"})
	if err != nil {
		return nil, err
	}
	out := make([]APIIssue, 0, len(rows))
	for _, m := range rows {
		var is APIIssue
		var err error
		if is.ID, err = strconv.Atoi(m["id"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: bad id %q", m["id"])
		}
		if is.ProjectID, err = strconv.Atoi(m["project_id"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: issue %d: bad project_id", is.ID)
		}
		is.Tracker, is.Status, is.Priority = m["tracker_name"], m["status_name"], m["priority_name"]
		is.HasAssignee = m["assigned_to_id"] != ""
		if v := m["fixed_version_name"]; v != "" {
			is.Version = &v
		}
		if v := m["start_date"]; v != "" {
			is.StartDate = &v
		}
		if v := m["due_date"]; v != "" {
			is.DueDate = &v
		}
		if is.Parent, err = optInt(m["parent_id"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: issue %d: %w", is.ID, err)
		}
		if is.DoneRatio, err = optInt(m["done_ratio"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: issue %d: %w", is.ID, err)
		}
		if is.Estimated, err = optFloat(m["estimated_hours"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: issue %d: %w", is.ID, err)
		}
		if is.Spent, err = optFloat(m["spent_hours"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: issue %d: %w", is.ID, err)
		}
		if is.Updated, err = time.Parse(time.RFC3339, m["updated_on"]); err != nil {
			return nil, fmt.Errorf("issue-index.csv: issue %d updated_on: %w", is.ID, err)
		}
		if v := m["closed_on"]; v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return nil, fmt.Errorf("issue-index.csv: issue %d closed_on: %w", is.ID, err)
			}
			is.ClosedOn = &t
		}
		is.Closed = optBool(m["is_closed"])
		is.Private = optBool(m["is_private"])
		out = append(out, is)
	}
	return out, nil
}

func optBool(s string) *bool {
	switch s {
	case "true":
		b := true
		return &b
	case "false":
		b := false
		return &b
	}
	return nil
}
