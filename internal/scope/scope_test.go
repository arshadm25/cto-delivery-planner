package scope

import (
	"strings"
	"testing"
)

func TestRepoScopeFileIsValid(t *testing.T) {
	s, err := LoadFile("../../config/scope.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ProjectIDs) != 38 || len(s.ExcludedProjectIDs) != 9 {
		t.Fatalf("included=%d excluded=%d", len(s.ProjectIDs), len(s.ExcludedProjectIDs))
	}
	if !s.Includes(14) || s.Includes(26) || s.Includes(1) {
		t.Fatal("Includes gave the wrong answer")
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":       `{"project_ids": []}`,
		"negative":    `{"project_ids": [-1]}`,
		"duplicate":   `{"project_ids": [2, 2]}`,
		"overlap":     `{"project_ids": [2], "excluded_project_ids": [2]}`,
		"unknown key": `{"project_ids": [2], "names": ["x"]}`,
	}
	for name, doc := range cases {
		if _, err := Load(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
