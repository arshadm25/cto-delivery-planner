package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/forecast"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
	"github.com/arshadm25/cto-delivery-planner/internal/risks"
)

// Delivery is a scope's baseline, forecast and actual dates with the
// forecast's explanation. The three dates are always separate.
type Delivery struct {
	Baseline       *time.Time     `json:"baseline"`
	BaselineSource *string        `json:"baseline_source"`
	CommitmentID   *int64         `json:"commitment_id"`
	Forecast       *time.Time     `json:"forecast"`
	Actual         *time.Time     `json:"actual"`
	Confidence     string         `json:"confidence"`
	Method         string         `json:"method"`
	Status         string         `json:"status"`
	Assumptions    []string       `json:"assumptions"`
	Inputs         map[string]any `json:"inputs"`
}

// liveCommitment is the approved or delivered commitment for a scope.
func liveCommitment(cs []planning.Commitment, projectID int, versionID *int) *planning.Commitment {
	for i := range cs {
		c := &cs[i]
		if c.ProjectID != projectID || (c.Status != "approved" && c.Status != "delivered") {
			continue
		}
		if (c.VersionID == nil && versionID == nil) || (c.VersionID != nil && versionID != nil && *c.VersionID == *versionID) {
			return c
		}
	}
	return nil
}

// capacityHours prorates approved allocations for a project over
// [from, to] by working days. It returns -1 when no approved allocation
// for the project overlaps the window.
func capacityHours(allocs []planning.Allocation, cal forecast.Calendar, projectID int, from, to time.Time) float64 {
	total, found := 0.0, false
	for _, a := range allocs {
		h, ok := a.Projects[projectID]
		if !ok || a.Status != "approved" {
			continue
		}
		total += prorate(a, cal, h, from, to, &found)
	}
	if !found {
		return -1
	}
	return total
}

func prorate(a planning.Allocation, cal forecast.Calendar, hours float64, from, to time.Time, found *bool) float64 {
	start, end := a.PeriodStart, a.PeriodEnd
	if end.Before(from) || start.After(to) {
		return 0
	}
	*found = true
	periodDays := cal.WorkingDaysBetween(start.AddDate(0, 0, -1), end)
	if periodDays == 0 {
		return 0
	}
	os, oe := start, end
	if from.After(os) {
		os = from
	}
	if to.Before(oe) {
		oe = to
	}
	overlap := cal.WorkingDaysBetween(os.AddDate(0, 0, -1), oe)
	return hours * float64(overlap) / float64(periodDays)
}

func (s *Server) addDelivery(ctx context.Context, a *analysis) error {
	var err error
	if a.calendar, err = s.plan.Calendar(ctx); err != nil {
		return err
	}
	ids := make([]int, len(a.d.Projects))
	for i, p := range a.d.Projects {
		ids[i] = p.ID
	}
	if a.commitments, err = s.plan.Commitments(ctx, ids); err != nil {
		return err
	}
	today := a.rules.Today
	if a.allocations, err = s.plan.Allocations(ctx, today, today.AddDate(1, 0, 0)); err != nil {
		return err
	}
	a.versionDelivery = map[int64]*Delivery{}
	issueByID := map[int]*risks.Issue{}
	for i := range a.d.Issues {
		issueByID[a.d.Issues[i].ID] = &a.d.Issues[i]
	}

	for _, p := range a.d.Projects {
		issues := a.d.ProjectIssues(p.ID)
		weekly := capacityHours(a.allocations, a.calendar, p.ID, today, today.AddDate(0, 0, 27)) / 4
		if weekly < 0 {
			weekly = 0
		}
		c := liveCommitment(a.commitments, p.ID, nil)
		d, flags := s.deliveryFor(a, p.ID, issues, issueByID, weekly, c)
		a.projects[p.ID].Delivery = d
		a.flags[p.ID] = append(a.flags[p.ID], flags...)
		if c == nil {
			a.flags[p.ID] = append(a.flags[p.ID], risks.Flag{Kind: risks.KindNoBaseline, Severity: risks.Low, ProjectID: p.ID,
				Title: "No approved delivery baseline", Evidence: "No approved commitment is recorded for this project",
				Action: "Record the approved baseline date (P4-07) or confirm there is none."})
		}

		// Version-level commitments get their own forecast.
		for i := range a.commitments {
			vc := &a.commitments[i]
			if vc.ProjectID != p.ID || vc.VersionID == nil || (vc.Status != "approved" && vc.Status != "delivered") {
				continue
			}
			var scoped []risks.Issue
			for _, is := range a.d.Issues {
				if is.VersionID != nil && *is.VersionID == *vc.VersionID {
					scoped = append(scoped, is)
				}
			}
			vd, vflags := s.deliveryFor(a, p.ID, scoped, issueByID, weekly, vc)
			for j := range vflags {
				vflags[j].VersionID = vc.VersionID
			}
			a.versionDelivery[vc.ID] = vd
			a.flags[p.ID] = append(a.flags[p.ID], vflags...)
		}
	}
	return nil
}

func (s *Server) deliveryFor(a *analysis, projectID int, issues []risks.Issue, issueByID map[int]*risks.Issue,
	weekly float64, c *planning.Commitment) (*Delivery, []risks.Flag) {

	inScope := map[int]bool{}
	for _, is := range issues {
		inScope[is.ID] = true
	}
	var blockerDue []time.Time
	for _, rel := range a.d.Relations {
		if (rel.Type != "blocks" && rel.Type != "precedes") || !inScope[rel.To] || inScope[rel.From] {
			continue
		}
		if b := issueByID[rel.From]; b != nil && !b.IsClosed && b.DueDate != nil {
			blockerDue = append(blockerDue, *b.DueDate)
		}
	}
	in := forecast.Input{Today: a.rules.Today, Issues: issues, AwaitingQA: a.rules.AwaitingQA, BlockerDue: blockerDue, WeeklyCapacity: weekly,
		CapacityUntilBaseline: -1, Calendar: a.calendar}
	var src *string
	var cid *int64
	if c != nil {
		b := c.BaselineDate
		in.Baseline = &b
		in.Actual = c.ActualDate
		in.CapacityUntilBaseline = capacityHours(a.allocations, a.calendar, projectID, a.rules.Today, b)
		label := fmt.Sprintf("Commitment #%d: %s", c.ID, c.Title)
		src, cid = &label, &c.ID
	}
	r := forecast.Compute(in)
	for i := range r.Flags {
		r.Flags[i].ProjectID = projectID
	}
	return &Delivery{Baseline: r.Baseline, BaselineSource: src, CommitmentID: cid, Forecast: r.Forecast, Actual: r.Actual,
		Confidence: r.Confidence, Method: r.Method, Status: r.Status, Assumptions: r.Assumptions, Inputs: r.Inputs}, r.Flags
}

func (s *Server) attachCommitments(ctx context.Context, a *analysis, projectID int, ms []MilestoneView) error {
	for i := range ms {
		vid := ms[i].ID
		if c := liveCommitment(a.commitments, projectID, &vid); c != nil {
			ms[i].Commitment = c
			ms[i].Delivery = a.versionDelivery[c.ID]
		}
	}
	return nil
}

// Commitments.

func (s *Server) handleListCommitments(w http.ResponseWriter, r *http.Request) {
	acc := current(r).access
	d, err := s.store.Dataset(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pid, _ := strconv.Atoi(r.URL.Query().Get("project"))
	var ids []int
	for _, p := range d.Projects {
		if acc.Allows(p.ID) && (pid == 0 || p.ID == pid) {
			ids = append(ids, p.ID)
		}
	}
	cs, err := s.plan.Commitments(r.Context(), ids)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if cs == nil {
		cs = []planning.Commitment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commitments": cs})
}

func (s *Server) planActor(r *http.Request) planning.Actor {
	u := current(r).user
	return planning.Actor{ID: u.ID, Email: u.Email}
}

func (s *Server) planError(w http.ResponseWriter, r *http.Request, err error) {
	var inv *planning.ErrInvalid
	switch {
	case errors.As(err, &inv):
		writeError(w, http.StatusBadRequest, inv.Msg)
	case errors.Is(err, planning.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		s.internal(w, r, err)
	}
}

func (s *Server) handleProposeCommitment(w http.ResponseWriter, r *http.Request) {
	var in planning.ProposeInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !current(r).access.Allows(in.ProjectID) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	d, err := s.store.Dataset(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if d.ProjectByID[in.ProjectID] == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if in.VersionID != nil && d.VersionByID[*in.VersionID] == nil {
		writeError(w, http.StatusBadRequest, "unknown version")
		return
	}
	c, err := s.plan.Propose(r.Context(), s.planActor(r), in)
	if err != nil {
		s.planError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// commitmentFor loads a commitment and checks project access.
func (s *Server) commitmentFor(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return 0, false
	}
	c, err := s.plan.Commitment(r.Context(), int64(id))
	if err != nil || !current(r).access.Allows(c.ProjectID) {
		writeError(w, http.StatusNotFound, "not found")
		return 0, false
	}
	return c.ID, true
}

func (s *Server) handleDecideCommitment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.commitmentFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := s.plan.Decide(r.Context(), s.planActor(r), id, in.Approve, in.Note)
	if err != nil {
		s.planError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeliveredCommitment(w http.ResponseWriter, r *http.Request) {
	id, ok := s.commitmentFor(w, r)
	if !ok {
		return
	}
	var in struct {
		ActualDate string `json:"actual_date"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := s.plan.MarkDelivered(r.Context(), s.planActor(r), id, in.ActualDate)
	if err != nil {
		s.planError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// Capacity.

func (s *Server) handleListAllocations(w http.ResponseWriter, r *http.Request) {
	from, to := s.period(r, 0, 90)
	as, err := s.plan.Allocations(r.Context(), from, to)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	acc := current(r).access
	out := []planning.Allocation{}
	for _, a := range as {
		visible := acc.All
		for pid := range a.Projects {
			if acc.Allows(pid) {
				visible = true
			} else if !acc.All {
				delete(a.Projects, pid)
			}
		}
		if visible {
			out = append(out, a)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"allocations": out, "from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly)})
}

func (s *Server) handleSaveAllocation(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.PathValue("id") != "" {
		n, ok := pathInt(r, "id")
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		id = int64(n)
	}
	var in planning.AllocationInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	acc := current(r).access
	for pid := range in.Projects {
		if !acc.Allows(pid) {
			writeError(w, http.StatusForbidden, fmt.Sprintf("you cannot allocate to project %d", pid))
			return
		}
	}
	id, err := s.plan.SaveAllocation(r.Context(), s.planActor(r), id, in)
	if err != nil {
		s.planError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "draft"})
}

func (s *Server) handleApproveAllocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.plan.ApproveAllocation(r.Context(), s.planActor(r), int64(id)); err != nil {
		s.planError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAllocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.plan.DeleteAllocation(r.Context(), s.planActor(r), int64(id)); err != nil {
		s.planError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// period reads from/to query dates, defaulting to [today+fromDays, today+toDays].
func (s *Server) period(r *http.Request, fromDays, toDays int) (time.Time, time.Time) {
	today := s.today()
	from, to := today.AddDate(0, 0, fromDays), today.AddDate(0, 0, toDays)
	if t, err := time.Parse(time.DateOnly, r.URL.Query().Get("from")); err == nil {
		from = t
	}
	if t, err := time.Parse(time.DateOnly, r.URL.Query().Get("to")); err == nil && !t.Before(from) {
		to = t
	}
	return from, to
}

// Calendar.

func (s *Server) handleGetCalendar(w http.ResponseWriter, r *http.Request) {
	c, err := s.plan.Calendar(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handlePutCalendar(w http.ResponseWriter, r *http.Request) {
	var c forecast.Calendar
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.plan.SaveCalendar(r.Context(), s.planActor(r), c); err != nil {
		s.planError(w, r, err)
		return
	}
	s.store.Invalidate()
	c.Confirmed = true
	writeJSON(w, http.StatusOK, c)
}
