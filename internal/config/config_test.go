package config

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestLoggerRedacts(t *testing.T) {
	t.Setenv("REDMINE_API_KEY", "abcdef0123456789-fake-key")
	var buf bytes.Buffer
	log := NewLogger(&buf, "test")
	log.Info("calling with abcdef0123456789-fake-key", "api_key", "x", "password", "hunter2hunter2",
		"error", errors.New("GET failed for abcdef0123456789-fake-key"), "path", "/issues.json")
	out := buf.String()
	for _, leak := range []string{"abcdef0123456789-fake-key", "hunter2hunter2"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %q in %s", leak, out)
		}
	}
	if !strings.Contains(out, "/issues.json") {
		t.Fatal("non-sensitive value lost")
	}
}
