package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
)

// WorkloadConcern is a person whose work due in a period exceeds their
// approved available hours, or whose allocations exceed availability.
type WorkloadConcern struct {
	PersonID                *int     `json:"person_id"`
	PersonName              string   `json:"person_name"`
	PeriodStart             string   `json:"period_start"`
	PeriodEnd               string   `json:"period_end"`
	AvailableHours          *float64 `json:"available_hours"`
	AllocatedHours          float64  `json:"allocated_hours"`
	RemainingEstimatedHours *float64 `json:"remaining_estimated_hours"`
	Reason                  string   `json:"reason"`
}

// PersonLoad is one row of the workload view. It is not a ranking: rows
// are sorted by name, and issue counts are shown next to hours because
// count alone is not a measure of load.
type PersonLoad struct {
	PersonID   int    `json:"person_id"`
	PersonName string `json:"person_name"`
	OpenIssues int    `json:"open_issues"`
	// RemainingEstimatedHours sums estimate x (1 - done ratio) for
	// estimated open issues; null if none are estimated.
	RemainingEstimatedHours *float64 `json:"remaining_estimated_hours"`
	UnestimatedOpen         int      `json:"unestimated_open"`
	// DueInPeriodHours is remaining estimate for issues due inside the period.
	DueInPeriodHours *float64 `json:"due_in_period_hours"`
	// LoggedHours28d is time logged in the last 28 days, or null when time
	// entries are not imported.
	LoggedHours28d *float64         `json:"logged_hours_28d"`
	AvailableHours *float64         `json:"available_hours"`
	AllocatedHours float64          `json:"allocated_hours"`
	CapacityStatus string           `json:"capacity_status"`
	Projects       map[int]int      `json:"open_by_project"`
	Concern        *WorkloadConcern `json:"concern,omitempty"`
}

// WorkloadView is the team workload page.
type WorkloadView struct {
	PeriodStart string       `json:"period_start"`
	PeriodEnd   string       `json:"period_end"`
	People      []PersonLoad `json:"people"`
	Unassigned  []struct {
		ProjectID   int    `json:"project_id"`
		ProjectName string `json:"project_name"`
		Open        int    `json:"open"`
		Unestimated int    `json:"unestimated"`
	} `json:"unassigned"`
	TimeEntriesAvailable bool     `json:"time_entries_available"`
	Notes                []string `json:"notes"`
}

func (s *Server) buildWorkload(ctx context.Context, acc auth.Access, a *analysis, from, to time.Time) (*WorkloadView, error) {
	out := &WorkloadView{PeriodStart: from.Format(time.DateOnly), PeriodEnd: to.Format(time.DateOnly), People: []PersonLoad{},
		Notes: []string{"Rows are sorted by name. This view is for planning capacity, not for comparing people."}}
	var visible []int
	people := map[int]*PersonLoad{}
	get := func(id int, name string) *PersonLoad {
		p := people[id]
		if p == nil {
			p = &PersonLoad{PersonID: id, PersonName: name, Projects: map[int]int{}}
			people[id] = p
		}
		return p
	}
	type un struct{ open, unest int }
	unassigned := map[int]*un{}
	for _, pr := range a.d.Projects {
		if !acc.Allows(pr.ID) {
			continue
		}
		visible = append(visible, pr.ID)
		for _, is := range a.d.ProjectIssues(pr.ID) {
			// Work awaiting QA is not development load.
			if is.IsClosed || a.rules.AwaitingQA(is) {
				continue
			}
			if is.AssigneeID == nil {
				u := unassigned[pr.ID]
				if u == nil {
					u = &un{}
					unassigned[pr.ID] = u
				}
				u.open++
				if is.Estimated == nil {
					u.unest++
				}
				continue
			}
			name := fmt.Sprintf("User %d", *is.AssigneeID)
			if is.AssigneeName != nil {
				name = *is.AssigneeName
			}
			p := get(*is.AssigneeID, name)
			p.OpenIssues++
			p.Projects[pr.ID]++
			if is.Estimated == nil {
				p.UnestimatedOpen++
				continue
			}
			done := 0.0
			if is.DoneRatio != nil {
				done = float64(*is.DoneRatio) / 100
			}
			rem := *is.Estimated * (1 - done)
			p.RemainingEstimatedHours = addPtr(p.RemainingEstimatedHours, rem)
			if is.DueDate != nil && !is.DueDate.After(to) {
				p.DueInPeriodHours = addPtr(p.DueInPeriodHours, rem)
			}
		}
	}
	for pid, u := range unassigned {
		out.Unassigned = append(out.Unassigned, struct {
			ProjectID   int    `json:"project_id"`
			ProjectName string `json:"project_name"`
			Open        int    `json:"open"`
			Unestimated int    `json:"unestimated"`
		}{pid, a.d.ProjectByID[pid].Name, u.open, u.unest})
	}
	sort.Slice(out.Unassigned, func(i, j int) bool { return out.Unassigned[i].ProjectName < out.Unassigned[j].ProjectName })

	// Capacity from approved allocations, prorated to the period.
	for _, al := range a.allocations {
		if al.PersonID == nil || al.Status != "approved" {
			continue
		}
		name := fmt.Sprintf("User %d", *al.PersonID)
		if al.PersonName != nil {
			name = *al.PersonName
		}
		p := get(*al.PersonID, name)
		var found bool
		if al.AvailableHours != nil {
			p.AvailableHours = addPtr(p.AvailableHours, prorate(al, a.calendar, *al.AvailableHours, from, to, &found))
		}
		for pid, h := range al.Projects {
			if acc.Allows(pid) {
				p.AllocatedHours += prorate(al, a.calendar, h, from, to, &found)
			}
		}
	}

	present, err := s.plan.TimeEntriesPresent(ctx)
	if err != nil {
		return nil, err
	}
	out.TimeEntriesAvailable = present
	if present {
		logged, err := s.plan.LoggedHours(ctx, visible, a.rules.Today.AddDate(0, 0, -27), a.rules.Today)
		if err != nil {
			return nil, err
		}
		for id, h := range logged {
			if p := people[id]; p != nil {
				v := h
				p.LoggedHours28d = &v
			}
		}
		for _, p := range people {
			if p.LoggedHours28d == nil {
				zero := 0.0
				p.LoggedHours28d = &zero
			}
		}
	} else {
		out.Notes = append(out.Notes, "Logged hours are not shown: no time entries were imported (the service account may not be allowed to read them).")
	}
	if !a.calendar.Confirmed {
		out.Notes = append(out.Notes, "The working calendar is not confirmed yet; capacity proration assumes the default working days.")
	}

	for _, p := range people {
		switch {
		case p.AvailableHours == nil:
			p.CapacityStatus = "unknown"
		case p.DueInPeriodHours != nil && *p.DueInPeriodHours > *p.AvailableHours:
			p.CapacityStatus = "over"
			p.Concern = s.concern(p, from, to, fmt.Sprintf("%.0f h of estimated work due by %s, %.0f h available",
				*p.DueInPeriodHours, to.Format(time.DateOnly), *p.AvailableHours))
		case p.AllocatedHours > *p.AvailableHours+0.01:
			p.CapacityStatus = "over"
			p.Concern = s.concern(p, from, to, fmt.Sprintf("allocations total %.0f h against %.0f h available", p.AllocatedHours, *p.AvailableHours))
		default:
			p.CapacityStatus = "within"
		}
		out.People = append(out.People, *p)
	}
	sort.Slice(out.People, func(i, j int) bool { return out.People[i].PersonName < out.People[j].PersonName })
	return out, nil
}

func (s *Server) concern(p *PersonLoad, from, to time.Time, reason string) *WorkloadConcern {
	id := p.PersonID
	return &WorkloadConcern{PersonID: &id, PersonName: p.PersonName, PeriodStart: from.Format(time.DateOnly), PeriodEnd: to.Format(time.DateOnly),
		AvailableHours: p.AvailableHours, AllocatedHours: p.AllocatedHours, RemainingEstimatedHours: p.DueInPeriodHours, Reason: reason}
}

func addPtr(p *float64, v float64) *float64 {
	if p == nil {
		return &v
	}
	n := *p + v
	return &n
}

func (s *Server) workloadConcerns(ctx context.Context, acc auth.Access, a *analysis) ([]WorkloadConcern, error) {
	wl, err := s.buildWorkload(ctx, acc, a, a.rules.Today, a.rules.Today.AddDate(0, 0, 27))
	if err != nil {
		return nil, err
	}
	out := []WorkloadConcern{}
	for _, p := range wl.People {
		if p.Concern != nil {
			out = append(out, *p.Concern)
		}
	}
	return out, nil
}

func (s *Server) handleWorkload(w http.ResponseWriter, r *http.Request) {
	a, err := s.analyze(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	from, to := s.period(r, 0, 27)
	wl, err := s.buildWorkload(r.Context(), current(r).access, a, from, to)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, wl)
}

// handlePeople lists Redmine people known to the mirror (ID and display
// name only) for capacity forms.
func (s *Server) handlePeople(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Pool.Query(r.Context(), `SELECT redmine_id, name FROM redmine_users ORDER BY name`)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	type person struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	out := []person{}
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			s.internal(w, r, err)
			return
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}
