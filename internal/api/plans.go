package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/planning"
	"github.com/arshadm25/cto-delivery-planner/internal/store"
)

// Planning in the planner (decided 2026-10-24): estimates, due dates,
// assignees and milestones set here replace Redmine's values in every view.
// Nothing is written back to Redmine.

const maxPlanIssues = 500

// handlePlanIssues applies one change to one or more issues of a project.
// Each key in "changes" that is present is applied; null goes back to the
// Redmine value.
func (s *Server) handlePlanIssues(w http.ResponseWriter, r *http.Request) {
	a, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	var in struct {
		IssueIDs []int                      `json:"issue_ids"`
		Changes  map[string]json.RawMessage `json:"changes"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(in.IssueIDs) == 0 || len(in.IssueIDs) > maxPlanIssues {
		writeError(w, http.StatusBadRequest, "choose between 1 and 500 issues")
		return
	}
	issues := a.d.ProjectIssues(ps.ID)
	inProject := map[int]bool{}
	for _, is := range issues {
		inProject[is.ID] = true
	}
	for _, id := range in.IssueIDs {
		if !inProject[id] {
			writeError(w, http.StatusBadRequest, "issue "+itoa(id)+" is not in this project")
			return
		}
	}

	var c store.PlanChange
	for k, raw := range in.Changes {
		null := strings.TrimSpace(string(raw)) == "null"
		var err error
		switch k {
		case "estimated_hours":
			c.Estimated.Set = true
			if !null {
				var v float64
				if err = json.Unmarshal(raw, &v); err == nil && (v < 0 || v > 10000) {
					err = errors.New("estimated_hours must be between 0 and 10000")
				}
				c.Estimated.Value = &v
			}
		case "due_date":
			c.DueDate.Set = true
			if !null {
				var v string
				var t time.Time
				if err = json.Unmarshal(raw, &v); err == nil {
					if t, err = time.Parse(time.DateOnly, v); err != nil {
						err = errors.New("due_date must be YYYY-MM-DD")
					}
				}
				c.DueDate.Value = &t
			}
		case "assignee_id":
			c.Assignee.Set = true
			if !null {
				var v int
				if err = json.Unmarshal(raw, &v); err == nil {
					var known bool
					if qerr := s.store.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM redmine_users WHERE redmine_id = $1)`, v).Scan(&known); qerr != nil {
						s.internal(w, r, qerr)
						return
					}
					if !known {
						err = errors.New("unknown assignee")
					}
				}
				c.Assignee.Value = &v
			}
		case "version_id":
			c.VersionID.Set = true
			if !null {
				var v int
				if err = json.Unmarshal(raw, &v); err == nil {
					allowed := false
					for _, ver := range s.projectVersions(a.d, ps.ID, issues) {
						allowed = allowed || ver.ID == v
					}
					if !allowed {
						err = errors.New("unknown milestone for this project")
					}
				}
				c.VersionID.Value = &v
			}
		default:
			err = errors.New("unknown field " + k)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if !c.Estimated.Set && !c.DueDate.Set && !c.Assignee.Set && !c.VersionID.Set {
		writeError(w, http.StatusBadRequest, "nothing to change")
		return
	}
	if err := s.store.SetIssuePlans(r.Context(), in.IssueIDs, c, current(r).user.ID, actorOf(r)); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type milestoneBody struct {
	Name    string  `json:"name"`
	DueDate *string `json:"due_date"`
	Status  string  `json:"status"`
}

func (b milestoneBody) input() (store.MilestoneInput, error) {
	in := store.MilestoneInput{Name: strings.TrimSpace(b.Name), Status: b.Status}
	if in.Name == "" {
		return in, errors.New("a milestone needs a name")
	}
	if in.Status == "" {
		in.Status = "open"
	}
	if in.Status != "open" && in.Status != "closed" {
		return in, errors.New("status must be open or closed")
	}
	if b.DueDate != nil && *b.DueDate != "" {
		t, err := time.Parse(time.DateOnly, *b.DueDate)
		if err != nil {
			return in, errors.New("due_date must be YYYY-MM-DD")
		}
		in.DueDate = &t
	}
	return in, nil
}

// handleSaveMilestone creates (POST) or updates (PUT, {mid}) a planner
// milestone. {mid} is the planner ID, which is minus the version ID.
func (s *Server) handleSaveMilestone(w http.ResponseWriter, r *http.Request) {
	_, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	var mid int
	if r.PathValue("mid") != "" {
		if mid, ok = pathInt(r, "mid"); !ok {
			writeError(w, http.StatusNotFound, "milestone not found")
			return
		}
	}
	var b milestoneBody
	if err := readJSON(r, &b); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in, err := b.input()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	vid, err := s.store.SaveMilestone(r.Context(), ps.ID, int64(mid), in, current(r).user.ID, actorOf(r))
	if errors.Is(err, store.ErrPlanNotFound) {
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	status := http.StatusOK
	if mid == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"id": vid})
}

func (s *Server) handleDeleteMilestone(w http.ResponseWriter, r *http.Request) {
	_, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	mid, ok := pathInt(r, "mid")
	if !ok {
		writeError(w, http.StatusNotFound, "milestone not found")
		return
	}
	err := s.store.DeleteMilestone(r.Context(), ps.ID, int64(mid), actorOf(r))
	switch {
	case errors.Is(err, store.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "milestone not found")
	case errors.Is(err, store.ErrMilestoneCommitted):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.internal(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := s.plan.Teams(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if teams == nil {
		teams = []planning.Team{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": teams})
}

func (s *Server) handleSaveTeam(w http.ResponseWriter, r *http.Request) {
	var id int
	if r.PathValue("id") != "" {
		var ok bool
		if id, ok = pathInt(r, "id"); !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
	}
	var in planning.TeamInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tid, err := s.plan.SaveTeam(r.Context(), s.planActor(r), int64(id), in)
	if err != nil {
		s.planError(w, r, err)
		return
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"id": tid})
}

func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.plan.DeleteTeam(r.Context(), s.planActor(r), int64(id)); err != nil {
		s.planError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
