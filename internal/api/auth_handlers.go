package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/arshadm25/cto-delivery-planner/internal/audit"
	"github.com/arshadm25/cto-delivery-planner/internal/auth"
)

type meResponse struct {
	User        auth.User         `json:"user"`
	Permissions []auth.Permission `json:"permissions"`
	AllProjects bool              `json:"all_projects"`
	ProjectIDs  []int             `json:"project_ids"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	now := s.cfg.Now()
	keys := []string{"ip:" + clientIP(r), "email:" + strings.ToLower(strings.TrimSpace(in.Email))}
	for _, k := range keys {
		if s.login.blocked(k, now) {
			writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
			return
		}
	}
	u, err := s.auth.Authenticate(r.Context(), in.Email, in.Password)
	if errors.Is(err, auth.ErrInvalidLogin) {
		for _, k := range keys {
			s.login.fail(k, now)
		}
		s.log.Warn("login failed", "ip", clientIP(r))
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	token, err := s.auth.NewSession(r.Context(), u.ID, s.cfg.SessionTTL)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(s.cfg.SessionTTL.Seconds()),
	})
	audit.Record(r.Context(), s.store.Pool, audit.Actor(u.ID, u.Email), "login", "app_users", itoa64(u.ID), nil, nil)
	s.writeMe(w, r, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.auth.EndSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	s.writeMe(w, r, current(r).user)
}

func (s *Server) writeMe(w http.ResponseWriter, r *http.Request, u auth.User) {
	out := meResponse{User: u, Permissions: u.Permissions(), AllProjects: u.SeesAllProjects(), ProjectIDs: []int{}}
	if !u.SeesAllProjects() {
		ids, err := s.auth.AssignedProjects(r.Context(), u.ID)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		out.ProjectIDs = ids
	}
	writeJSON(w, http.StatusOK, out)
}

// Admin: users.

type userWithProjects struct {
	auth.User
	ProjectIDs []int `json:"project_ids"`
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	us, err := s.auth.ListUsers(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]userWithProjects, len(us))
	for i, u := range us {
		ids, err := s.auth.AssignedProjects(r.Context(), u.ID)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		out[i] = userWithProjects{u, ids}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string    `json:"email"`
		DisplayName string    `json:"display_name"`
		Password    string    `json:"password"`
		Role        auth.Role `json:"role"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	u, err := s.auth.CreateUser(r.Context(), in.Email, in.DisplayName, in.Password, in.Role)
	switch {
	case errors.Is(err, auth.ErrDuplicate):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil && strings.HasPrefix(err.Error(), "auth:"):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	me := current(r).user
	audit.Record(r.Context(), s.store.Pool, audit.Actor(me.ID, me.Email), "create", "app_users", itoa64(u.ID), nil, u)
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		DisplayName *string    `json:"display_name"`
		Role        *auth.Role `json:"role"`
		Disabled    *bool      `json:"disabled"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	me := current(r).user
	if int64(id) == me.ID && ((in.Disabled != nil && *in.Disabled) || (in.Role != nil && *in.Role != auth.Admin)) {
		writeError(w, http.StatusBadRequest, "you cannot disable or demote your own account")
		return
	}
	u, err := s.auth.UpdateUser(r.Context(), int64(id), in.DisplayName, in.Role, in.Disabled)
	if err != nil && strings.HasPrefix(err.Error(), "auth:") {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	audit.Record(r.Context(), s.store.Pool, audit.Actor(me.ID, me.Email), "update", "app_users", itoa64(u.ID), nil, in)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleSetUserProjects(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		ProjectIDs []int `json:"project_ids"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.auth.SetAssignedProjects(r.Context(), int64(id), in.ProjectIDs); err != nil {
		s.internal(w, r, err)
		return
	}
	me := current(r).user
	audit.Record(r.Context(), s.store.Pool, audit.Actor(me.ID, me.Email), "set_projects", "app_users", itoa(id), nil, in)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.auth.SetPassword(r.Context(), int64(id), in.Password); errors.Is(err, auth.ErrWeakPassword) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		s.internal(w, r, err)
		return
	}
	me := current(r).user
	audit.Record(r.Context(), s.store.Pool, audit.Actor(me.ID, me.Email), "reset_password", "app_users", itoa(id), nil, nil)
	w.WriteHeader(http.StatusNoContent)
}
