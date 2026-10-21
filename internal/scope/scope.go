// Package scope loads the list of Redmine project IDs the planner covers.
//
// The file lists IDs only. Project names stay out of the repository.
package scope

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// Scope is the parsed config/scope.json.
type Scope struct {
	DecidedOn          string `json:"decided_on"`
	DecidedBy          string `json:"decided_by"`
	Rule               string `json:"rule"`
	IncludeSubprojects bool   `json:"include_subprojects"`
	ProjectIDs         []int  `json:"project_ids"`
	ExcludedProjectIDs []int  `json:"excluded_project_ids"`
}

// Includes reports whether a project ID is in scope.
func (s *Scope) Includes(id int) bool {
	i := sort.SearchInts(s.ProjectIDs, id)
	return i < len(s.ProjectIDs) && s.ProjectIDs[i] == id
}

// LoadFile reads and validates a scope file.
func LoadFile(path string) (*Scope, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f)
}

// Load reads and validates a scope document. IDs must be positive, unique,
// and not listed as both included and excluded.
func Load(r io.Reader) (*Scope, error) {
	var s Scope
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("scope: %w", err)
	}
	if len(s.ProjectIDs) == 0 {
		return nil, fmt.Errorf("scope: project_ids is empty")
	}
	seen := map[int]string{}
	check := func(ids []int, list string) error {
		for _, id := range ids {
			if id <= 0 {
				return fmt.Errorf("scope: %s has invalid project ID %d", list, id)
			}
			if prev, ok := seen[id]; ok {
				return fmt.Errorf("scope: project ID %d appears in %s and %s", id, prev, list)
			}
			seen[id] = list
		}
		return nil
	}
	if err := check(s.ProjectIDs, "project_ids"); err != nil {
		return nil, err
	}
	if err := check(s.ExcludedProjectIDs, "excluded_project_ids"); err != nil {
		return nil, err
	}
	sort.Ints(s.ProjectIDs)
	sort.Ints(s.ExcludedProjectIDs)
	return &s, nil
}
