package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
	"github.com/arshadm25/cto-delivery-planner/internal/risks"
)

// TakeSnapshots stores today's state of every in-scope project and prunes
// snapshots older than retentionDays (Q12).
func (s *Server) TakeSnapshots(ctx context.Context, retentionDays int) (int, error) {
	a, err := s.analyze(ctx)
	if err != nil {
		return 0, err
	}
	now := s.cfg.Now()
	var snaps []planning.Snapshot
	for _, p := range a.d.Projects {
		ps := a.projects[p.ID]
		sn := planning.Snapshot{Date: a.rules.Today, ProjectID: p.ID, TakenAt: now, Health: string(ps.Health.State),
			HealthReasons: ps.Health.Reasons, Open: ps.Issues.Open, Closed: ps.Issues.Closed, Overdue: ps.Issues.Overdue,
			Blocked: ps.Issues.Blocked, RemainingEstimatedHours: ps.RemainingEstimatedHours, EstimateCoverage: ps.EstimateCoverage,
			FlagCounts: ps.FlagCounts, Confidence: "none", Method: "tbd"}
		if d := ps.Delivery; d != nil {
			sn.Baseline, sn.Forecast, sn.Confidence, sn.Method = d.Baseline, d.Forecast, d.Confidence, d.Method
		}
		if sn.HealthReasons == nil {
			sn.HealthReasons = []string{}
		}
		snaps = append(snaps, sn)
	}
	if len(snaps) == 0 {
		return 0, nil
	}
	pruned, err := s.plan.SaveSnapshots(ctx, snaps, retentionDays)
	if err == nil && pruned > 0 {
		s.log.Info("snapshots pruned", "rows", pruned, "retention_days", retentionDays)
	}
	return len(snaps), err
}

// RunSnapshotLoop takes one snapshot per day, checking hourly.
func (s *Server) RunSnapshotLoop(ctx context.Context, retentionDays int, log *slog.Logger) {
	var last time.Time
	for {
		today := s.today()
		if !today.Equal(last) {
			if n, err := s.TakeSnapshots(ctx, retentionDays); err != nil {
				log.Error("snapshot failed", "error", err.Error())
			} else {
				last = today
				log.Info("snapshot taken", "projects", n, "date", today.Format(time.DateOnly))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}

func (s *Server) handleTakeSnapshot(w http.ResponseWriter, r *http.Request) {
	n, err := s.TakeSnapshots(r.Context(), 0)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": n, "date": s.today().Format(time.DateOnly)})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || !current(r).access.Allows(id) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 3650 {
		days = 90
	}
	h, err := s.plan.History(r.Context(), id, s.today().AddDate(0, 0, -days))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if h == nil {
		h = []planning.Snapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": h})
}

// ProjectChange compares a project now with its previous snapshot.
type ProjectChange struct {
	ProjectID      int        `json:"project_id"`
	ProjectName    string     `json:"project_name"`
	HealthBefore   *string    `json:"health_before"`
	HealthNow      string     `json:"health_now"`
	ForecastBefore *time.Time `json:"forecast_before"`
	ForecastNow    *time.Time `json:"forecast_now"`
	Baseline       *time.Time `json:"baseline"`
	Causes         []string   `json:"causes"`
}

// WeeklyReport is plan section 10.5.
type WeeklyReport struct {
	ReportDate        string            `json:"report_date"`
	ComparedWith      *string           `json:"compared_with"`
	Changes           []ProjectChange   `json:"changes"`
	ForecastChanges   []ProjectChange   `json:"forecast_changes"`
	MilestonesDue     []MilestoneRef    `json:"milestones_due"`
	Blockers          []FlagView        `json:"blockers"`
	CapacityConflicts []WorkloadConcern `json:"capacity_conflicts"`
	Decisions         []string          `json:"decisions_needed"`
	DataQuality       []string          `json:"data_quality_asks"`
	Notes             []string          `json:"notes"`
}

func (s *Server) buildWeekly(ctx context.Context, acc auth.Access, maskPeople bool) (*WeeklyReport, error) {
	a, err := s.analyze(ctx)
	if err != nil {
		return nil, err
	}
	today := a.rules.Today
	rep := &WeeklyReport{ReportDate: today.Format(time.DateOnly), Changes: []ProjectChange{}, ForecastChanges: []ProjectChange{},
		MilestonesDue: []MilestoneRef{}, Blockers: []FlagView{}, Decisions: []string{}, DataQuality: []string{}, Notes: []string{}}

	prev, err := s.plan.SnapshotsOn(ctx, today.AddDate(0, 0, -7))
	if err != nil {
		return nil, err
	}
	if len(prev) == 0 {
		prev, err = s.plan.SnapshotsOn(ctx, today.AddDate(0, 0, -1))
		if err != nil {
			return nil, err
		}
	}
	before := map[int]planning.Snapshot{}
	for _, sn := range prev {
		before[sn.ProjectID] = sn
	}
	if len(prev) > 0 {
		d := prev[0].Date.Format(time.DateOnly)
		rep.ComparedWith = &d
	} else {
		rep.Notes = append(rep.Notes, "No earlier snapshot exists yet, so changes cannot be shown. Snapshots are taken daily.")
	}

	for _, p := range a.d.Projects {
		if !acc.Allows(p.ID) {
			continue
		}
		ps := a.projects[p.ID]
		ch := ProjectChange{ProjectID: p.ID, ProjectName: p.Name, HealthNow: string(ps.Health.State), Causes: []string{}}
		if d := ps.Delivery; d != nil {
			ch.ForecastNow, ch.Baseline = d.Forecast, d.Baseline
		}
		if sn, ok := before[p.ID]; ok {
			h := sn.Health
			ch.HealthBefore = &h
			ch.ForecastBefore = sn.Forecast
			ch.Causes = causes(sn, ps)
		}
		healthChanged := ch.HealthBefore != nil && *ch.HealthBefore != ch.HealthNow
		forecastChanged := ch.HealthBefore != nil && !sameDate(ch.ForecastBefore, ch.ForecastNow)
		if healthChanged || forecastChanged {
			rep.Changes = append(rep.Changes, ch)
		}
		if forecastChanged {
			rep.ForecastChanges = append(rep.ForecastChanges, ch)
		}

		issues := a.d.ProjectIssues(p.ID)
		var versions []risks.Version
		for _, v := range s.projectVersions(a.d, p.ID, issues) {
			if v.ProjectID == p.ID {
				versions = append(versions, *v)
			}
		}
		for _, m := range milestoneRefs(versions, issues) {
			if m.Status != "closed" && m.Open > 0 && m.DueDate != nil && !m.DueDate.Before(today) && !m.DueDate.After(today.AddDate(0, 0, 14)) {
				rep.MilestonesDue = append(rep.MilestonesDue, m)
			}
		}
		for _, f := range a.flags[p.ID] {
			if f.Kind == risks.KindBlocked || ((f.Kind == risks.KindForecastLate || f.Kind == risks.KindCapacity) && f.Severity == risks.High) {
				fv := FlagView{Flag: f, ProjectName: p.Name}
				if f.IssueID != nil {
					fv.RedmineURL = s.issueURL(*f.IssueID)
				}
				rep.Blockers = append(rep.Blockers, fv)
			}
			if f.Kind == risks.KindForecastLate {
				rep.Decisions = append(rep.Decisions, fmt.Sprintf("%s: forecast is after the approved baseline. Decide on scope, capacity or a revised baseline.", p.Name))
			}
		}
		if ps.Delivery != nil && ps.Delivery.Baseline == nil {
			rep.Decisions = append(rep.Decisions, fmt.Sprintf("%s: no approved delivery baseline recorded.", p.Name))
		}
		var gaps []string
		if n := ps.Issues.OpenUnassigned; n > 0 {
			gaps = append(gaps, fmt.Sprintf("%d without assignee", n))
		}
		if n := ps.Issues.OpenNoEstimate; n > 0 {
			gaps = append(gaps, fmt.Sprintf("%d without estimate", n))
		}
		if n := ps.Issues.OpenNoDate; n > 0 {
			gaps = append(gaps, fmt.Sprintf("%d without due date", n))
		}
		if n := ps.FlagCounts[risks.KindStale]; n > 0 {
			gaps = append(gaps, fmt.Sprintf("%d with no update for %d+ days", n, a.rules.StaleAfterDays))
		}
		if ps.Owner == nil || ps.TechLead == nil {
			gaps = append(gaps, "owner or tech lead TBD")
		}
		if len(gaps) > 0 {
			rep.DataQuality = append(rep.DataQuality, fmt.Sprintf("%s: %s", p.Name, strings.Join(gaps, ", ")))
		}
	}
	sort.Slice(rep.MilestonesDue, func(i, j int) bool { return rep.MilestonesDue[i].DueDate.Before(*rep.MilestonesDue[j].DueDate) })
	sort.SliceStable(rep.Blockers, func(i, j int) bool {
		return severityRank(rep.Blockers[i].Severity) < severityRank(rep.Blockers[j].Severity)
	})

	for _, c := range a.commitments {
		if c.Status == "proposed" && acc.Allows(c.ProjectID) {
			kind := "New baseline"
			if c.SupersedesID != nil {
				kind = "Revised baseline"
			}
			rep.Decisions = append(rep.Decisions, fmt.Sprintf("%s proposed for %s: %q on %s (reason: %s).", kind,
				a.d.ProjectByID[c.ProjectID].Name, c.Title, c.BaselineDate.Format(time.DateOnly), c.Reason))
		}
	}
	drafts := 0
	for _, al := range a.allocations {
		if al.Status == "draft" {
			drafts++
		}
	}
	if drafts > 0 {
		rep.Decisions = append(rep.Decisions, fmt.Sprintf("%d capacity allocation(s) are waiting for approval.", drafts))
	}

	rep.CapacityConflicts, err = s.workloadConcerns(ctx, acc, a)
	if err != nil {
		return nil, err
	}
	if maskPeople {
		for i := range rep.CapacityConflicts {
			c := &rep.CapacityConflicts[i]
			if c.PersonID != nil {
				c.PersonName = "User #" + strconv.Itoa(*c.PersonID)
			}
		}
		rep.Notes = append(rep.Notes, "People are shown by Redmine user ID so the report can be shared.")
	}
	return rep, nil
}

func causes(sn planning.Snapshot, ps *ProjectSummary) []string {
	var out []string
	if d := ps.Issues.Open - sn.Open; d != 0 {
		out = append(out, fmt.Sprintf("open issues %+d (%d to %d)", d, sn.Open, ps.Issues.Open))
	}
	if d := ps.Issues.Closed - sn.Closed; d > 0 {
		out = append(out, fmt.Sprintf("%d issue(s) closed", d))
	}
	if a, b := sn.RemainingEstimatedHours, ps.RemainingEstimatedHours; a != nil && b != nil && *a != *b {
		out = append(out, fmt.Sprintf("remaining estimate %.0f h to %.0f h", *a, *b))
	}
	if a, b := sn.EstimateCoverage, ps.EstimateCoverage; a != nil && b != nil && int(*a*100) != int(*b*100) {
		out = append(out, fmt.Sprintf("estimate coverage %.0f%% to %.0f%%", *a*100, *b*100))
	}
	if d := ps.Issues.Overdue - sn.Overdue; d != 0 {
		out = append(out, fmt.Sprintf("overdue issues %+d", d))
	}
	if d := ps.Issues.Blocked - sn.Blocked; d != 0 {
		out = append(out, fmt.Sprintf("blocked issues %+d", d))
	}
	if ps.Delivery != nil && !sameDate(sn.Baseline, ps.Delivery.Baseline) {
		out = append(out, "approved baseline changed")
	}
	if ps.Delivery != nil && sn.Method != ps.Delivery.Method {
		out = append(out, fmt.Sprintf("forecast method %s to %s", sn.Method, ps.Delivery.Method))
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func sameDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Format(time.DateOnly) == b.Format(time.DateOnly)
}

func (s *Server) handleWeeklyReport(w http.ResponseWriter, r *http.Request) {
	md := r.URL.Query().Get("format") == "md"
	// Markdown is meant to be shared, so people are masked unless asked.
	mask := md && r.URL.Query().Get("names") != "full"
	rep, err := s.buildWeekly(r.Context(), current(r).access, mask)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !md {
		writeJSON(w, http.StatusOK, rep)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="weekly-report-%s.md"`, rep.ReportDate))
	w.Write([]byte(rep.Markdown()))
}

// Markdown renders the report for sharing.
func (rep *WeeklyReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Weekly delivery report, %s\n\n", rep.ReportDate)
	if rep.ComparedWith != nil {
		fmt.Fprintf(&b, "Compared with the snapshot of %s.\n\n", *rep.ComparedWith)
	}
	for _, n := range rep.Notes {
		fmt.Fprintf(&b, "_%s_\n\n", n)
	}
	date := func(t *time.Time) string {
		if t == nil {
			return "TBD"
		}
		return t.Format(time.DateOnly)
	}
	section := func(title string, empty bool, body func()) {
		fmt.Fprintf(&b, "## %s\n\n", title)
		if empty {
			b.WriteString("None.\n\n")
			return
		}
		body()
		b.WriteString("\n")
	}
	section("What changed", len(rep.Changes) == 0, func() {
		for _, c := range rep.Changes {
			hb := "new"
			if c.HealthBefore != nil {
				hb = *c.HealthBefore
			}
			fmt.Fprintf(&b, "- **%s**: health %s to %s", c.ProjectName, hb, c.HealthNow)
			if len(c.Causes) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(c.Causes, "; "))
			}
			b.WriteString("\n")
		}
	})
	section("Forecast changes", len(rep.ForecastChanges) == 0, func() {
		for _, c := range rep.ForecastChanges {
			fmt.Fprintf(&b, "- **%s**: forecast %s to %s, baseline %s. Causes: %s\n", c.ProjectName, date(c.ForecastBefore),
				date(c.ForecastNow), date(c.Baseline), strings.Join(c.Causes, "; "))
		}
	})
	section("Milestones due in the next 14 days", len(rep.MilestonesDue) == 0, func() {
		for _, m := range rep.MilestonesDue {
			fmt.Fprintf(&b, "- %s (project %d): %s, %d of %d issues open\n", m.Name, m.ProjectID, date(m.DueDate), m.Open, m.Total)
		}
	})
	section("Blockers and forecast risks", len(rep.Blockers) == 0, func() {
		for _, f := range rep.Blockers {
			fmt.Fprintf(&b, "- [%s] %s: %s. %s\n", f.Severity, f.ProjectName, f.Title, f.Evidence)
		}
	})
	section("Capacity conflicts", len(rep.CapacityConflicts) == 0, func() {
		for _, c := range rep.CapacityConflicts {
			fmt.Fprintf(&b, "- %s, %s to %s: %s\n", c.PersonName, c.PeriodStart, c.PeriodEnd, c.Reason)
		}
	})
	section("Decisions needed", len(rep.Decisions) == 0, func() {
		for _, d := range rep.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	})
	section("Data-quality asks for project managers", len(rep.DataQuality) == 0, func() {
		for _, d := range rep.DataQuality {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	})
	return b.String()
}
