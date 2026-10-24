package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/forecast"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
	"github.com/arshadm25/cto-delivery-planner/internal/risks"
)

// Delivery is a scope's baseline, forecast and actual dates with the
// forecast's explanation. The three dates are always separate.
type Delivery struct {
	Baseline       *time.Time `json:"baseline"`
	BaselineSource *string    `json:"baseline_source"`
	CommitmentID   *int64     `json:"commitment_id"`
	Forecast       *time.Time `json:"forecast"`
	// DevDone is when development finishes; Forecast adds the QA days.
	DevDone     *time.Time     `json:"dev_done"`
	Actual      *time.Time     `json:"actual"`
	Confidence  string         `json:"confidence"`
	Method      string         `json:"method"`
	Status      string         `json:"status"`
	Assumptions []string       `json:"assumptions"`
	Inputs      map[string]any `json:"inputs"`
	// CoversProjectIDs lists the projects a parent's forecast includes:
	// itself and its in-scope subprojects. Empty for a single project.
	CoversProjectIDs []int `json:"covers_project_ids,omitempty"`
	// Scope is set when the baseline froze its scope on approval.
	Scope *ScopeChange `json:"scope,omitempty"`
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
	var live []int64
	for _, c := range a.commitments {
		if c.Status == "approved" || c.Status == "delivered" {
			live = append(live, c.ID)
		}
	}
	scopes, err := s.plan.Scopes(ctx, live)
	if err != nil {
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
		// A parent project is planned together with its in-scope
		// subprojects: one baseline, one forecast and their summed capacity.
		tree := a.d.Subtree(p.ID)
		var issues []risks.Issue
		for _, id := range tree {
			issues = append(issues, a.d.ProjectIssues(id)...)
		}
		weekly := weeklyCapacity(a, tree)
		c := liveCommitment(a.commitments, p.ID, nil)
		d, flags := s.deliveryFor(a, p.ID, tree, issues, issueByID, deliveryOpts{weekly: weekly, c: c, scope: scopes})
		if len(tree) > 1 {
			d.CoversProjectIDs = tree
			own, _ := s.deliveryFor(a, p.ID, []int{p.ID}, a.d.ProjectIssues(p.ID), issueByID,
				deliveryOpts{weekly: weeklyCapacity(a, []int{p.ID}), c: c, scope: scopes})
			a.projects[p.ID].ownDelivery = own
		}
		a.projects[p.ID].Delivery = d
		a.flags[p.ID] = append(a.flags[p.ID], flags...)
		if c == nil {
			a.flags[p.ID] = append(a.flags[p.ID], risks.Flag{Kind: risks.KindNoBaseline, Severity: risks.Low, ProjectID: p.ID,
				Title: "No approved delivery baseline", Evidence: "No approved commitment is recorded for this project",
				Action: "Record the approved baseline date (P4-07) or confirm there is none."})
		}

		// Milestone commitments get their own forecast. The same people
		// deliver them one after another in baseline order, so each one's
		// forecast starts after the open work of the milestones before it.
		var vcs []*planning.Commitment
		for i := range a.commitments {
			vc := &a.commitments[i]
			if vc.ProjectID == p.ID && vc.VersionID != nil && (vc.Status == "approved" || vc.Status == "delivered") {
				vcs = append(vcs, vc)
			}
		}
		sort.SliceStable(vcs, func(i, j int) bool { return vcs[i].BaselineDate.Before(vcs[j].BaselineDate) })
		var aheadHours float64
		var aheadOpen int
		for _, vc := range vcs {
			var scoped []risks.Issue
			for _, is := range a.d.Issues {
				if is.VersionID != nil && *is.VersionID == *vc.VersionID {
					scoped = append(scoped, is)
				}
			}
			vd, vflags := s.deliveryFor(a, p.ID, tree, scoped, issueByID,
				deliveryOpts{weekly: weekly, c: vc, scope: scopes, aheadHours: aheadHours, aheadOpen: aheadOpen})
			for j := range vflags {
				vflags[j].VersionID = vc.VersionID
			}
			a.versionDelivery[vc.ID] = vd
			a.flags[p.ID] = append(a.flags[p.ID], vflags...)
			if vc.Status == "approved" {
				h, n := openWork(scoped, a.rules.AwaitingQA)
				aheadHours += h
				aheadOpen += n
			}
		}
	}
	return nil
}

// weeklyCapacity is approved hours per week over the next four weeks for
// the given projects.
func weeklyCapacity(a *analysis, projectIDs []int) float64 {
	today := a.rules.Today
	var total float64
	for _, id := range projectIDs {
		if h := capacityHours(a.allocations, a.calendar, id, today, today.AddDate(0, 0, 27)); h > 0 {
			total += h
		}
	}
	return total / 4
}

// openWork is the remaining development effort, scaled up for unestimated
// issues as the forecast does, and the open development issue count.
func openWork(issues []risks.Issue, awaitingQA func(risks.Issue) bool) (float64, int) {
	var rem float64
	open, est := 0, 0
	for _, is := range issues {
		if is.IsClosed || awaitingQA(is) {
			continue
		}
		open++
		if is.Estimated != nil {
			est++
			done := 0.0
			if is.DoneRatio != nil {
				done = float64(*is.DoneRatio) / 100
			}
			rem += *is.Estimated * (1 - done)
		}
	}
	if est == 0 {
		return 0, open
	}
	return rem * float64(open) / float64(est), open
}

type deliveryOpts struct {
	weekly     float64
	c          *planning.Commitment
	scope      map[int64][]planning.ScopeItem
	aheadHours float64
	aheadOpen  int
}

func (s *Server) deliveryFor(a *analysis, projectID int, projectIDs []int, issues []risks.Issue, issueByID map[int]*risks.Issue,
	o deliveryOpts) (*Delivery, []risks.Flag) {

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
	in := forecast.Input{Today: a.rules.Today, Issues: issues, AwaitingQA: a.rules.AwaitingQA, BlockerDue: blockerDue, WeeklyCapacity: o.weekly,
		CapacityUntilBaseline: -1, Calendar: a.calendar, AheadHours: o.aheadHours, AheadOpen: o.aheadOpen, QADays: a.rules.QADays}
	var src *string
	var cid *int64
	var scope *ScopeChange
	if c := o.c; c != nil {
		b := c.BaselineDate
		in.Baseline = &b
		in.Actual = c.ActualDate
		in.CapacityUntilBaseline = -1
		for _, id := range projectIDs {
			if h := capacityHours(a.allocations, a.calendar, id, a.rules.Today, b); h >= 0 {
				in.CapacityUntilBaseline = max(in.CapacityUntilBaseline, 0) + h
			}
		}
		label := fmt.Sprintf("Commitment #%d: %s", c.ID, c.Title)
		src, cid = &label, &c.ID
		if items, ok := o.scope[c.ID]; ok {
			scope = scopeChange(items, issues)
		}
	}
	r := forecast.Compute(in)
	for i := range r.Flags {
		r.Flags[i].ProjectID = projectID
	}
	return &Delivery{Baseline: r.Baseline, BaselineSource: src, CommitmentID: cid, Forecast: r.Forecast, DevDone: r.DevDone, Actual: r.Actual,
		Confidence: r.Confidence, Method: r.Method, Status: r.Status, Assumptions: r.Assumptions, Inputs: r.Inputs, Scope: scope}, r.Flags
}

// ScopeChange compares the work in a commitment's scope now with the work
// frozen when it was approved.
type ScopeChange struct {
	BaselineIssues int     `json:"baseline_issues"`
	BaselineHours  float64 `json:"baseline_hours"`
	CurrentIssues  int     `json:"current_issues"`
	CurrentHours   float64 `json:"current_hours"`
	Added          []int   `json:"added"`
	Removed        []int   `json:"removed"`
	// EstimateChangeHours is how much estimates of issues in both moved.
	EstimateChangeHours float64 `json:"estimate_change_hours"`
}

func scopeChange(items []planning.ScopeItem, issues []risks.Issue) *ScopeChange {
	sc := &ScopeChange{Added: []int{}, Removed: []int{}}
	frozen := map[int]planning.ScopeItem{}
	for _, it := range items {
		frozen[it.IssueID] = it
		sc.BaselineIssues++
		if it.Estimated != nil {
			sc.BaselineHours += *it.Estimated
		}
	}
	now := map[int]bool{}
	for _, is := range issues {
		now[is.ID] = true
		sc.CurrentIssues++
		if is.Estimated != nil {
			sc.CurrentHours += *is.Estimated
		}
		it, was := frozen[is.ID]
		if !was {
			sc.Added = append(sc.Added, is.ID)
			continue
		}
		var before, after float64
		if it.Estimated != nil {
			before = *it.Estimated
		}
		if is.Estimated != nil {
			after = *is.Estimated
		}
		sc.EstimateChangeHours += after - before
	}
	for _, it := range items {
		if !now[it.IssueID] {
			sc.Removed = append(sc.Removed, it.IssueID)
		}
	}
	sort.Ints(sc.Added)
	sort.Ints(sc.Removed)
	return sc
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
	var scope []planning.ScopeItem
	if in.Approve {
		var err error
		if scope, err = s.commitmentScope(r, id); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	c, err := s.plan.Decide(r.Context(), s.planActor(r), id, in.Approve, in.Note, scope)
	if err != nil {
		s.planError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// commitmentScope is the work a commitment covers right now, with the
// planner's values: a milestone's issues, or a project's and its
// subprojects' issues for a whole-project commitment.
func (s *Server) commitmentScope(r *http.Request, id int64) ([]planning.ScopeItem, error) {
	c, err := s.plan.Commitment(r.Context(), id)
	if err != nil {
		return nil, err
	}
	d, err := s.store.Dataset(r.Context())
	if err != nil {
		return nil, err
	}
	var issues []risks.Issue
	if c.VersionID != nil {
		for _, is := range d.Issues {
			if is.VersionID != nil && *is.VersionID == *c.VersionID {
				issues = append(issues, is)
			}
		}
	} else {
		for _, pid := range d.Subtree(c.ProjectID) {
			issues = append(issues, d.ProjectIssues(pid)...)
		}
	}
	out := make([]planning.ScopeItem, len(issues))
	for i, is := range issues {
		out[i] = planning.ScopeItem{IssueID: is.ID, Estimated: is.Estimated, Open: !is.IsClosed}
	}
	return out, nil
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
