package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/forecast"
	"github.com/arshadm25/cto-delivery-planner/internal/planning"
	"github.com/arshadm25/cto-delivery-planner/internal/risks"
	"github.com/arshadm25/cto-delivery-planner/internal/store"
)

// analysis is the rules applied to one dataset.
type analysis struct {
	d        *store.Dataset
	rules    risks.Rules
	flags    map[int][]risks.Flag // by project
	byIssue  map[int][]string     // issue -> flag kinds
	projects map[int]*ProjectSummary
	// Phase 4 inputs, loaded by addDelivery.
	calendar        forecast.Calendar
	commitments     []planning.Commitment
	allocations     []planning.Allocation
	versionDelivery map[int64]*Delivery // by commitment ID
}

// IssueCounts are open/closed totals and data gaps for open issues.
type IssueCounts struct {
	Total          int `json:"total"`
	Open           int `json:"open"`
	Closed         int `json:"closed"`
	Overdue        int `json:"overdue"`
	Blocked        int `json:"blocked"`
	OpenUnassigned int `json:"open_unassigned"`
	OpenNoEstimate int `json:"open_no_estimate"`
	OpenNoDate     int `json:"open_no_date"`
}

// MilestoneRef is a short milestone description.
type MilestoneRef struct {
	ID        int        `json:"id"`
	ProjectID int        `json:"project_id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	DueDate   *time.Time `json:"due_date"`
	Open      int        `json:"open"`
	Total     int        `json:"total"`
}

// ProjectSummary is the per-project row on the overview.
type ProjectSummary struct {
	store.Project
	Health risks.Health `json:"health"`
	Issues IssueCounts  `json:"issues"`
	// RemainingEstimatedHours sums estimates of open issues that have one.
	// It is null when no open issue has an estimate; see EstimateCoverage.
	RemainingEstimatedHours *float64 `json:"remaining_estimated_hours"`
	// EstimateCoverage is the share of open issues with an estimate, or
	// null when nothing is open.
	EstimateCoverage *float64       `json:"estimate_coverage"`
	SpentHours       *float64       `json:"spent_hours"`
	LastActivity     *time.Time     `json:"last_activity"`
	NextMilestone    *MilestoneRef  `json:"next_milestone"`
	FlagCounts       map[string]int `json:"flag_counts"`
	RedmineURL       string         `json:"redmine_url,omitempty"`
	// Delivery holds baseline, forecast and actual (Phase 4).
	Delivery *Delivery `json:"delivery,omitempty"`
}

func (s *Server) analyze(ctx context.Context) (*analysis, error) {
	d, err := s.store.Dataset(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := s.store.Rules(ctx, s.today())
	if err != nil {
		return nil, err
	}
	a := &analysis{d: d, rules: rules, flags: map[int][]risks.Flag{}, byIssue: map[int][]string{}, projects: map[int]*ProjectSummary{}}
	for i := range d.Projects {
		p := &d.Projects[i]
		issues := d.ProjectIssues(p.ID)
		var versions []risks.Version
		for _, v := range s.projectVersions(d, p.ID, issues) {
			versions = append(versions, *v)
		}
		fs := append(risks.IssueFlags(issues, d.Relations, rules), risks.MilestoneFlags(versions, issues, rules)...)
		for i := range fs {
			fs[i].ProjectID = p.ID
		}
		a.flags[p.ID] = fs
		a.projects[p.ID] = s.summarize(a, p, issues, versions)
	}
	if err := s.addDelivery(ctx, a); err != nil {
		return nil, err
	}
	for id, fs := range a.flags {
		risks.Sort(fs)
		for _, f := range fs {
			if f.IssueID != nil {
				a.byIssue[*f.IssueID] = append(a.byIssue[*f.IssueID], f.Kind)
			}
		}
		ps := a.projects[id]
		ps.FlagCounts = map[string]int{}
		for _, f := range fs {
			ps.FlagCounts[f.Kind]++
		}
		ps.Health = risks.ProjectHealth(ps.Issues.Open, hasSchedule(a.d.ProjectIssues(id), ps), fs)
	}
	return a, nil
}

func hasSchedule(issues []risks.Issue, ps *ProjectSummary) bool {
	for _, is := range issues {
		if !is.IsClosed && is.DueDate != nil {
			return true
		}
	}
	return ps.NextMilestone != nil && ps.NextMilestone.DueDate != nil
}

// projectVersions returns the project's own versions plus shared versions
// its issues target.
func (s *Server) projectVersions(d *store.Dataset, projectID int, issues []risks.Issue) []*risks.Version {
	seen := map[int]bool{}
	var out []*risks.Version
	for i := range d.Versions {
		if d.Versions[i].ProjectID == projectID {
			seen[d.Versions[i].ID] = true
			out = append(out, &d.Versions[i])
		}
	}
	for _, is := range issues {
		if is.VersionID != nil && !seen[*is.VersionID] {
			if v := d.VersionByID[*is.VersionID]; v != nil {
				seen[v.ID] = true
				out = append(out, v)
			}
		}
	}
	return out
}

func (s *Server) summarize(a *analysis, p *store.Project, issues []risks.Issue, versions []risks.Version) *ProjectSummary {
	ps := &ProjectSummary{Project: *p}
	if s.cfg.RedmineURL != "" {
		ps.RedmineURL = s.cfg.RedmineURL + "/projects/" + p.Identifier
	}
	var remaining, spent float64
	var anyEst, anySpent bool
	estimated := 0
	for _, is := range issues {
		ps.Issues.Total++
		if is.Spent != nil {
			spent += *is.Spent
			anySpent = true
		}
		if ps.LastActivity == nil || is.UpdatedOn.After(*ps.LastActivity) {
			t := is.UpdatedOn
			ps.LastActivity = &t
		}
		if is.IsClosed {
			ps.Issues.Closed++
			continue
		}
		ps.Issues.Open++
		if is.AssigneeID == nil {
			ps.Issues.OpenUnassigned++
		}
		if is.Estimated == nil {
			ps.Issues.OpenNoEstimate++
		} else {
			estimated++
			remaining += *is.Estimated
			anyEst = true
		}
		if is.DueDate == nil {
			ps.Issues.OpenNoDate++
		}
	}
	for _, f := range a.flags[p.ID] {
		switch f.Kind {
		case risks.KindOverdue:
			ps.Issues.Overdue++
		case risks.KindBlocked:
			ps.Issues.Blocked++
		}
	}
	if anyEst {
		ps.RemainingEstimatedHours = &remaining
	}
	if ps.Issues.Open > 0 {
		c := float64(estimated) / float64(ps.Issues.Open)
		ps.EstimateCoverage = &c
	}
	if anySpent {
		ps.SpentHours = &spent
	}
	ms := milestoneRefs(versions, issues)
	today := a.rules.Today
	for i := range ms {
		m := ms[i]
		if m.Status == "closed" || m.Open == 0 {
			continue
		}
		if m.DueDate != nil && !m.DueDate.Before(today) {
			if ps.NextMilestone == nil || ps.NextMilestone.DueDate == nil || m.DueDate.Before(*ps.NextMilestone.DueDate) {
				ps.NextMilestone = &m
			}
		}
	}
	return ps
}

func milestoneRefs(versions []risks.Version, issues []risks.Issue) []MilestoneRef {
	out := make([]MilestoneRef, len(versions))
	idx := map[int]int{}
	for i, v := range versions {
		out[i] = MilestoneRef{ID: v.ID, ProjectID: v.ProjectID, Name: v.Name, Status: v.Status, DueDate: v.Due}
		idx[v.ID] = i
	}
	for _, is := range issues {
		if is.VersionID == nil {
			continue
		}
		if i, ok := idx[*is.VersionID]; ok {
			out[i].Total++
			if !is.IsClosed {
				out[i].Open++
			}
		}
	}
	return out
}

func (s *Server) visibleSummaries(r *http.Request, a *analysis) []*ProjectSummary {
	acc := current(r).access
	var out []*ProjectSummary
	for _, p := range a.d.Projects {
		if acc.Allows(p.ID) {
			out = append(out, a.projects[p.ID])
		}
	}
	return out
}

// PortfolioSummary is the CTO overview.
type PortfolioSummary struct {
	ProjectsInScope    int               `json:"projects_in_scope"`
	ByHealth           map[string]int    `json:"by_health"`
	OverdueIssues      int               `json:"overdue_issues"`
	BlockedIssues      int               `json:"blocked_issues"`
	CriticalBlocked    int               `json:"critical_blocked"`
	OpenIssues         int               `json:"open_issues"`
	UpcomingMilestones []MilestoneRef    `json:"upcoming_milestones"`
	LastSuccessfulSync *time.Time        `json:"last_successful_sync"`
	DataStale          bool              `json:"data_stale"`
	Projects           []*ProjectSummary `json:"projects"`
	WorkloadConcerns   []WorkloadConcern `json:"workload_concerns"`
	Today              string            `json:"today"`
}

func (s *Server) handlePortfolio(w http.ResponseWriter, r *http.Request) {
	a, err := s.analyze(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	ps := s.visibleSummaries(r, a)
	out := PortfolioSummary{ProjectsInScope: len(ps), ByHealth: map[string]int{}, Projects: ps, UpcomingMilestones: []MilestoneRef{},
		Today: a.rules.Today.Format(time.DateOnly)}
	for _, st := range []risks.State{risks.OnTrack, risks.AtRisk, risks.Blocked, risks.InsufficientData} {
		out.ByHealth[string(st)] = 0
	}
	horizon := a.rules.Today.AddDate(0, 0, 30)
	for _, p := range ps {
		out.ByHealth[string(p.Health.State)]++
		out.OverdueIssues += p.Issues.Overdue
		out.BlockedIssues += p.Issues.Blocked
		out.OpenIssues += p.Issues.Open
		for _, f := range a.flags[p.ID] {
			if f.Kind == risks.KindBlocked && f.Severity == risks.Critical {
				out.CriticalBlocked++
			}
		}
		issues := a.d.ProjectIssues(p.ID)
		var versions []risks.Version
		for _, v := range s.projectVersions(a.d, p.ID, issues) {
			if v.ProjectID == p.ID {
				versions = append(versions, *v)
			}
		}
		for _, m := range milestoneRefs(versions, issues) {
			if m.Status != "closed" && m.Open > 0 && m.DueDate != nil && !m.DueDate.Before(a.rules.Today) && !m.DueDate.After(horizon) {
				out.UpcomingMilestones = append(out.UpcomingMilestones, m)
			}
		}
	}
	sort.Slice(out.UpcomingMilestones, func(i, j int) bool {
		return out.UpcomingMilestones[i].DueDate.Before(*out.UpcomingMilestones[j].DueDate)
	})
	out.LastSuccessfulSync, err = s.store.LastSuccessfulSync(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out.DataStale = out.LastSuccessfulSync == nil || s.cfg.Now().Sub(*out.LastSuccessfulSync) > s.cfg.SyncStaleAfter
	out.WorkloadConcerns, err = s.workloadConcerns(r.Context(), current(r).access, a)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	a, err := s.analyze(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	ps := s.visibleSummaries(r, a)
	if h := r.URL.Query().Get("health"); h != "" {
		var keep []*ProjectSummary
		for _, p := range ps {
			if string(p.Health.State) == h {
				keep = append(keep, p)
			}
		}
		ps = keep
	}
	if ps == nil {
		ps = []*ProjectSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

// projectFor resolves {id} and enforces access. Projects the user may not
// see answer 404, so their existence is not disclosed.
func (s *Server) projectFor(w http.ResponseWriter, r *http.Request) (*analysis, *ProjectSummary, bool) {
	id, ok := pathInt(r, "id")
	if !ok || !current(r).access.Allows(id) {
		writeError(w, http.StatusNotFound, "project not found")
		return nil, nil, false
	}
	a, err := s.analyze(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return nil, nil, false
	}
	ps := a.projects[id]
	if ps == nil {
		writeError(w, http.StatusNotFound, "project not found")
		return nil, nil, false
	}
	return a, ps, true
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	a, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	fs := a.flags[ps.ID]
	top := []risks.Flag{}
	for _, f := range fs {
		if f.Severity == risks.Critical || f.Severity == risks.High {
			top = append(top, f)
		}
	}
	if len(top) > 50 {
		top = top[:50]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":   ps,
		"blockers":  top,
		"data_gaps": dataGaps(ps),
		"rules":     a.rules,
	})
}

// dataGaps lists what limits forecast confidence for a project.
func dataGaps(ps *ProjectSummary) []string {
	gaps := []string{}
	if ps.Owner == nil {
		gaps = append(gaps, "Project owner is TBD")
	}
	if ps.TechLead == nil {
		gaps = append(gaps, "Technical lead is TBD")
	}
	if n := ps.Issues.OpenNoEstimate; n > 0 {
		gaps = append(gaps, strconv.Itoa(n)+" open issue(s) have no estimate")
	}
	if n := ps.Issues.OpenNoDate; n > 0 {
		gaps = append(gaps, strconv.Itoa(n)+" open issue(s) have no due date")
	}
	if n := ps.Issues.OpenUnassigned; n > 0 {
		gaps = append(gaps, strconv.Itoa(n)+" open issue(s) have no assignee")
	}
	if ps.NextMilestone == nil {
		gaps = append(gaps, "No upcoming dated milestone")
	}
	return gaps
}

// IssueView is an issue row with its flags and Redmine link.
type IssueView struct {
	risks.Issue
	VersionName *string  `json:"version_name"`
	Flags       []string `json:"flags"`
	RedmineURL  string   `json:"redmine_url,omitempty"`
}

func (s *Server) issueURL(id int) string {
	if s.cfg.RedmineURL == "" {
		return ""
	}
	return s.cfg.RedmineURL + "/issues/" + strconv.Itoa(id)
}

func (s *Server) handleIssues(w http.ResponseWriter, r *http.Request) {
	a, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	status := q.Get("status")
	if status == "" {
		status = "open"
	}
	search := strings.ToLower(strings.TrimSpace(q.Get("q")))
	flag := q.Get("flag")
	assignee := q.Get("assignee")
	version := q.Get("version")
	tracker := q.Get("tracker")

	var rows []IssueView
	for _, is := range a.d.ProjectIssues(ps.ID) {
		switch status {
		case "open":
			if is.IsClosed {
				continue
			}
		case "closed":
			if !is.IsClosed {
				continue
			}
		}
		if search != "" && !strings.Contains(strings.ToLower(is.Subject), search) && strconv.Itoa(is.ID) != search {
			continue
		}
		if tracker != "" && is.TrackerName != tracker {
			continue
		}
		if assignee == "none" && is.AssigneeID != nil {
			continue
		}
		if assignee != "" && assignee != "none" && (is.AssigneeID == nil || strconv.Itoa(*is.AssigneeID) != assignee) {
			continue
		}
		if version != "" && (is.VersionID == nil || strconv.Itoa(*is.VersionID) != version) {
			continue
		}
		kinds := a.byIssue[is.ID]
		if flag != "" && !contains(kinds, flag) {
			continue
		}
		v := IssueView{Issue: is, Flags: kinds, RedmineURL: s.issueURL(is.ID)}
		if v.Flags == nil {
			v.Flags = []string{}
		}
		if is.VersionID != nil {
			if ver := a.d.VersionByID[*is.VersionID]; ver != nil {
				v.VersionName = &ver.Name
			}
		}
		rows = append(rows, v)
	}
	sortIssues(rows, q.Get("sort"))
	page, per := paging(r)
	total := len(rows)
	start := (page - 1) * per
	if start > total {
		start = total
	}
	end := start + per
	if end > total {
		end = total
	}
	out := rows[start:end]
	if out == nil {
		out = []IssueView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": out, "total": total, "page": page, "per_page": per})
}

func sortIssues(rows []IssueView, by string) {
	less := func(a, b *time.Time) bool {
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.Before(*b)
	}
	switch by {
	case "due":
		sort.SliceStable(rows, func(i, j int) bool { return less(rows[i].DueDate, rows[j].DueDate) })
	case "updated":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].UpdatedOn.After(rows[j].UpdatedOn) })
	default:
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	}
}

func paging(r *http.Request) (page, per int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	per, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if per < 1 || per > 200 {
		per = 50
	}
	return
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// MilestoneView is a version with progress and its flags.
type MilestoneView struct {
	MilestoneRef
	RemainingEstimatedHours *float64             `json:"remaining_estimated_hours"`
	OpenNoEstimate          int                  `json:"open_no_estimate"`
	Flags                   []risks.Flag         `json:"flags"`
	RedmineURL              string               `json:"redmine_url,omitempty"`
	Commitment              *planning.Commitment `json:"commitment,omitempty"`
	Delivery                *Delivery            `json:"delivery,omitempty"`
}

func (s *Server) handleMilestones(w http.ResponseWriter, r *http.Request) {
	a, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	issues := a.d.ProjectIssues(ps.ID)
	var versions []risks.Version
	for _, v := range s.projectVersions(a.d, ps.ID, issues) {
		versions = append(versions, *v)
	}
	refs := milestoneRefs(versions, issues)
	out := make([]MilestoneView, len(refs))
	for i, m := range refs {
		mv := MilestoneView{MilestoneRef: m, Flags: []risks.Flag{}}
		if s.cfg.RedmineURL != "" {
			mv.RedmineURL = s.cfg.RedmineURL + "/versions/" + strconv.Itoa(m.ID)
		}
		var rem float64
		any := false
		for _, is := range issues {
			if is.VersionID == nil || *is.VersionID != m.ID || is.IsClosed {
				continue
			}
			if is.Estimated == nil {
				mv.OpenNoEstimate++
			} else {
				rem += *is.Estimated
				any = true
			}
		}
		if any {
			mv.RemainingEstimatedHours = &rem
		}
		for _, f := range a.flags[ps.ID] {
			if f.IssueID == nil && f.VersionID != nil && *f.VersionID == m.ID {
				mv.Flags = append(mv.Flags, f)
			}
		}
		out[i] = mv
	}
	if err := s.attachCommitments(r.Context(), a, ps.ID, out); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"milestones": out})
}

// FlagView is a flag with links for the risks screen.
type FlagView struct {
	risks.Flag
	ProjectName string `json:"project_name"`
	RedmineURL  string `json:"redmine_url,omitempty"`
}

func (s *Server) handleRisks(w http.ResponseWriter, r *http.Request) {
	a, err := s.analyze(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	q := r.URL.Query()
	pid, _ := strconv.Atoi(q.Get("project"))
	kind, sev := q.Get("kind"), q.Get("severity")
	acc := current(r).access
	var all []FlagView
	counts := map[string]int{}
	for _, p := range a.d.Projects {
		if !acc.Allows(p.ID) || (pid != 0 && p.ID != pid) {
			continue
		}
		for _, f := range a.flags[p.ID] {
			if sev != "" && string(f.Severity) != sev {
				continue
			}
			counts[f.Kind]++
			if kind != "" && f.Kind != kind {
				continue
			}
			fv := FlagView{Flag: f, ProjectName: p.Name}
			if f.IssueID != nil {
				fv.RedmineURL = s.issueURL(*f.IssueID)
			} else if f.VersionID != nil && s.cfg.RedmineURL != "" {
				fv.RedmineURL = s.cfg.RedmineURL + "/versions/" + strconv.Itoa(*f.VersionID)
			}
			all = append(all, fv)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		return severityRank(all[i].Severity) < severityRank(all[j].Severity)
	})
	page, per := paging(r)
	total := len(all)
	start := min((page-1)*per, total)
	end := min(start+per, total)
	out := all[start:end]
	if out == nil {
		out = []FlagView{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": out, "total": total, "page": page, "per_page": per, "counts_by_kind": counts})
}

func severityRank(s risks.Severity) int {
	switch s {
	case risks.Critical:
		return 0
	case risks.High:
		return 1
	case risks.Medium:
		return 2
	}
	return 3
}

func (s *Server) handleSetRoles(w http.ResponseWriter, r *http.Request) {
	_, ps, ok := s.projectFor(w, r)
	if !ok {
		return
	}
	var in struct {
		Owner    *string `json:"owner"`
		TechLead *string `json:"tech_lead"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	clean := func(p *string) *string {
		if p == nil || strings.TrimSpace(*p) == "" {
			return nil
		}
		v := strings.TrimSpace(*p)
		return &v
	}
	me := current(r).user
	if err := s.store.SetProjectRoles(r.Context(), ps.ID, clean(in.Owner), clean(in.TechLead), me.ID, actorOf(r)); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.LoadSyncStatus(r.Context(), 20)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	stale := st.LastSuccess == nil || st.LastSuccess.FinishedAt == nil || s.cfg.Now().Sub(*st.LastSuccess.FinishedAt) > s.cfg.SyncStaleAfter
	var latestFailed bool
	if len(st.Runs) > 0 {
		o := st.Runs[0].Outcome
		latestFailed = o == "failed" || o == "partial" || o == "abandoned"
	}
	// Non-admin users only see reconciliation rows for their projects.
	acc := current(r).access
	rec := st.LatestReconciliation[:0]
	for _, x := range st.LatestReconciliation {
		if acc.Allows(x.ProjectID) {
			rec = append(rec, x)
		}
	}
	st.LatestReconciliation = rec
	if st.LatestReconciliation == nil {
		st.LatestReconciliation = []store.Reconciliation{}
	}
	if st.Runs == nil {
		st.Runs = []store.SyncRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":           st,
		"data_stale":       stale,
		"latest_failed":    latestFailed,
		"stale_after":      s.cfg.SyncStaleAfter.String(),
		"showing_previous": latestFailed && st.LastSuccess != nil,
	})
}

func (s *Server) handleGetRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.store.Rules(r.Context(), s.today())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handlePutRules(w http.ResponseWriter, r *http.Request) {
	var in risks.Rules
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.StaleAfterDays < 1 || in.StaleAfterDays > 365 || in.MilestoneWarnDays < 1 || in.MilestoneWarnDays > 365 {
		writeError(w, http.StatusBadRequest, "day thresholds must be between 1 and 365")
		return
	}
	if err := s.store.SaveRules(r.Context(), in, current(r).user.ID, actorOf(r)); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func actorOf(r *http.Request) string {
	u := current(r).user
	return "user:" + strconv.FormatInt(u.ID, 10) + " " + u.Email
}

func itoa(n int) string     { return strconv.Itoa(n) }
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
