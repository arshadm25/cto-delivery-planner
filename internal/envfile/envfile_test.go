package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := `# comment
REDMINE_URL=https://redmine.example.com
export REDMINE_PAGE_SIZE=50
QUOTED="with spaces"
SINGLE='x'
EMPTY=
ALREADY=from-file
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"REDMINE_URL", "REDMINE_PAGE_SIZE", "QUOTED", "SINGLE", "EMPTY"} {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Unsetenv(k) })
	}
	t.Setenv("ALREADY", "from-shell")

	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"REDMINE_URL":       "https://redmine.example.com",
		"REDMINE_PAGE_SIZE": "50",
		"QUOTED":            "with spaces",
		"SINGLE":            "x",
		"EMPTY":             "",
		"ALREADY":           "from-shell",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if len(set) != 5 {
		t.Errorf("set %v, want 5 names", set)
	}
}

func TestMissingFileIsFine(t *testing.T) {
	set, err := Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil || set != nil {
		t.Fatalf("set=%v err=%v", set, err)
	}
}

func TestMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(path, []byte("NOT A PAIR\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error")
	}
}
