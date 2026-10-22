// Package config reads settings from environment variables. Secrets are
// read but never printed.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
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

// Logger returns a JSON logger at LOG_LEVEL (debug, info, warn, error).
func Logger(service string) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(String("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})).With("service", service)
}

// Fatal logs and exits.
func Fatal(log *slog.Logger, msg string, err error) {
	log.Error(msg, "error", fmt.Sprint(err))
	os.Exit(1)
}
