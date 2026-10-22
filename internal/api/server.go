// Package api is the planner's REST API (plan section 9). It reads the
// PostgreSQL mirror and writes only planner-owned tables; it never talks to
// Redmine.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/store"
)

// Config holds API settings.
type Config struct {
	// CookieSecure sets the Secure flag; true everywhere except local http.
	CookieSecure bool
	SessionTTL   time.Duration
	// RedmineURL is the browser-facing Redmine base URL used to build issue
	// links. It is not a secret and no credential is attached.
	RedmineURL string
	// Location is the planner's time zone for "today" (Q7).
	Location *time.Location
	// SyncStaleAfter marks data stale when the last successful sync is older.
	SyncStaleAfter time.Duration
	Now            func() time.Time
}

// Server serves /api/v1.
type Server struct {
	cfg   Config
	store *store.Store
	auth  *auth.Store
	log   *slog.Logger
	mux   *http.ServeMux
	login *limiter
}

const sessionCookie = "planner_session"

// New builds the handler tree.
func New(cfg Config, st *store.Store, as *auth.Store, log *slog.Logger) *Server {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.Location == nil {
		cfg.Location = time.UTC
	}
	if cfg.SyncStaleAfter <= 0 {
		cfg.SyncStaleAfter = 2 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	cfg.RedmineURL = strings.TrimRight(cfg.RedmineURL, "/")
	s := &Server{cfg: cfg, store: st, auth: as, log: log, mux: http.NewServeMux(), login: newLimiter(10, 15*time.Minute)}
	s.routes()
	return s
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	m.HandleFunc("GET /readyz", s.ready)

	m.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	m.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	m.Handle("GET /api/v1/auth/me", s.authed(s.handleMe))

	m.Handle("GET /api/v1/portfolio/summary", s.authed(s.handlePortfolio))
	m.Handle("GET /api/v1/projects", s.authed(s.handleProjects))
	m.Handle("GET /api/v1/projects/{id}", s.authed(s.handleProject))
	m.Handle("GET /api/v1/projects/{id}/issues", s.authed(s.handleIssues))
	m.Handle("GET /api/v1/projects/{id}/milestones", s.authed(s.handleMilestones))
	m.Handle("PUT /api/v1/projects/{id}/roles", s.authed(s.require(auth.EditProjectRoles, s.handleSetRoles)))
	m.Handle("GET /api/v1/risks", s.authed(s.handleRisks))
	m.Handle("GET /api/v1/sync/status", s.authed(s.handleSyncStatus))

	m.Handle("GET /api/v1/settings/rules", s.authed(s.handleGetRules))
	m.Handle("PUT /api/v1/settings/rules", s.authed(s.require(auth.EditSettings, s.handlePutRules)))

	m.Handle("GET /api/v1/admin/users", s.authed(s.require(auth.ManageUsers, s.handleListUsers)))
	m.Handle("POST /api/v1/admin/users", s.authed(s.require(auth.ManageUsers, s.handleCreateUser)))
	m.Handle("PATCH /api/v1/admin/users/{id}", s.authed(s.require(auth.ManageUsers, s.handleUpdateUser)))
	m.Handle("PUT /api/v1/admin/users/{id}/projects", s.authed(s.require(auth.ManageUsers, s.handleSetUserProjects)))
	m.Handle("POST /api/v1/admin/users/{id}/password", s.authed(s.require(auth.ManageUsers, s.handleResetPassword)))

}

// ServeHTTP adds security headers, CSRF protection, panic recovery and
// request logging around the routes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := &statusRecorder{ResponseWriter: w, status: 200}
	h := rec.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Cache-Control", "no-store")
	defer func() {
		if p := recover(); p != nil {
			s.log.Error("panic", "path", r.URL.Path, "panic", p, "stack", string(debug.Stack()))
			writeError(rec, http.StatusInternalServerError, "internal error")
		}
		if r.URL.Path != "/healthz" {
			s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		}
	}()
	// Cookies are SameSite=Strict; this header is a second guard, because a
	// cross-site form cannot set it.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Requested-With") != "planner" {
		writeError(rec, http.StatusForbidden, "missing X-Requested-With header")
		return
	}
	r.Body = http.MaxBytesReader(rec, r.Body, 1<<20)
	s.mux.ServeHTTP(rec, r)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Pool.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	w.Write([]byte("ok\n"))
}

type ctxKey int

const userKey ctxKey = 1

type request struct {
	user   auth.User
	access auth.Access
}

func current(r *http.Request) *request { return r.Context().Value(userKey).(*request) }

func (s *Server) authed(h func(http.ResponseWriter, *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		u, err := s.auth.UserForToken(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		acc, err := s.auth.ProjectAccess(r.Context(), u)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userKey, &request{user: u, access: acc})))
	})
}

func (s *Server) require(p auth.Permission, h func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if !current(r).user.Can(p) {
			writeError(w, http.StatusForbidden, "your role does not allow this")
			return
		}
		h(w, r)
	}
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "path", r.URL.Path, "error", err.Error())
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func pathInt(r *http.Request, name string) (int, bool) {
	n, err := strconv.Atoi(r.PathValue(name))
	return n, err == nil && n > 0
}

// today is the calendar date in the planner's zone, as a UTC midnight.
func (s *Server) today() time.Time {
	t := s.cfg.Now().In(s.cfg.Location)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// limiter counts failed logins per key in a sliding window.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	l.hits[key] = keep
	return len(keep) >= l.max
}

func (l *limiter) fail(key string, now time.Time) {
	l.mu.Lock()
	// Bound memory if someone sprays random emails.
	if len(l.hits) > 10000 {
		l.hits = map[string][]time.Time{}
	}
	l.hits[key] = append(l.hits[key], now)
	l.mu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
