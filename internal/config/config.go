// Package config reads settings from environment variables. Secrets are
// read but never printed.
package config

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// String returns the variable or def when unset or empty.
func String(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// Int returns a positive integer variable or def.
func Int(name string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && n > 0 {
		return n
	}
	return def
}

// Duration returns a positive duration variable such as "30m" or def.
func Duration(name string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv(name))); err == nil && d > 0 {
		return d
	}
	return def
}

// Bool returns true for 1, true, yes or on, false for 0, false, no or off,
// and def otherwise.
func Bool(name string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

var (
	secretsMu sync.RWMutex
	secrets   []string
)

// RegisterSecret makes the logger replace this exact value wherever it
// appears in a log line. Values shorter than 8 characters are ignored.
func RegisterSecret(v string) {
	if len(v) < 8 {
		return
	}
	secretsMu.Lock()
	secrets = append(secrets, v)
	secretsMu.Unlock()
}

var sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|authorization|cookie)`)

// Redact scrubs registered secret values from s.
func Redact(s string) string {
	secretsMu.RLock()
	defer secretsMu.RUnlock()
	for _, v := range secrets {
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}

func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if sensitiveKey.MatchString(a.Key) {
		return slog.String(a.Key, "[redacted]")
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Redact(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, Redact(err.Error()))
		}
	}
	return a
}

// redactHandler also scrubs the message itself.
type redactHandler struct{ slog.Handler }

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Message = Redact(r.Message)
	return h.Handler.Handle(ctx, r)
}

func (h redactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return redactHandler{h.Handler.WithAttrs(as)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{h.Handler.WithGroup(name)}
}

// Logger returns a JSON logger at LOG_LEVEL (debug, info, warn, error) that
// redacts sensitive attribute names and registered secret values. The
// Redmine key and the database URL are registered automatically.
func Logger(service string) *slog.Logger {
	return NewLogger(os.Stdout, service)
}

// NewLogger is Logger writing to w.
func NewLogger(w io.Writer, service string) *slog.Logger {
	RegisterSecret(os.Getenv("REDMINE_API_KEY"))
	RegisterSecret(os.Getenv("DATABASE_URL"))
	var level slog.Level
	if err := level.UnmarshalText([]byte(String("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level, ReplaceAttr: redactAttr})
	return slog.New(redactHandler{h}).With("service", service)
}

// Fatal logs and exits.
func Fatal(log *slog.Logger, msg string, err error) {
	log.Error(msg, "error", fmt.Sprint(err))
	os.Exit(1)
}
