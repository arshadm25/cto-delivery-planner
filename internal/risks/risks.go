// Package risks computes blockers, data-quality flags and project health
// from mirrored Redmine data. The rules are deliberately simple so every
// flag can be explained: each one carries its evidence and a suggested
// next action, and links back to the source issue or milestone.
package risks

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Issue is the subset of a mirrored issue the rules read.
type Issue struct {
	ID           int        `json:"id"`
	ProjectID    int        `json:"project_id"`
	Subject      string     `json:"subject"`
	TrackerName  string     `json:"tracker"`
	StatusName   string     `json:"status"`
	PriorityName string     `json:"priority"`
	IsClosed     bool       `json:"is_closed"`
	AssigneeID   *int       `json:"assignee_id"`
	AssigneeName *string    `json:"assignee_name"`
	VersionID    *int       `json:"version_id"`
	StartDate    *time.Time `json:"start_date"`
	DueDate      *time.Time `json:"due_date"`
	DoneRatio    *int       `json:"done_ratio"`
	Estimated    *float64   `json:"estimated_hours"`
	Spent        *float64   `json:"spent_hours"`
	CreatedOn    time.Time  `json:"created_on"`
	UpdatedOn    time.Time  `json:"updated_on"`
	ClosedOn     *time.Time `json:"closed_on"`
}

// Relation is a stored issue relation.
type Relation struct {
	From int    `json:"from"`
	To   int    `json:"to"`
	Type string `json:"type"`
}

// Version is a milestone.
type Version struct {
	ID        int        `json:"id"`
	ProjectID int        `json:"project_id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Due       *time.Time `json:"due_date"`
}

// Rules are the tunable thresholds. Zero values are replaced by defaults.
type Rules struct {
	// Today is the calendar date in the planner's time zone.
	Today time.Time `json:"-"`
	// StaleAfterDays flags open issues with no update for this long (Q12).
	StaleAfterDays int `json:"stale_after_days"`
	// MilestoneWarnDays is how close a due milestone must be to be checked
	// for open work.
	MilestoneWarnDays int `json:"milestone_warn_days"`
	// HighPriorities are priority names treated as critical when blocked.
	HighPriorities []string `json:"high_priorities"`
	// ReleaseKeywords mark milestones or trackers that gate delivery,
	// such as QA, UAT, release and handover.
	ReleaseKeywords []string `json:"release_keywords"`
	// AwaitingQAStatuses are open status names meaning development is
	// finished and the work waits for testing. Such issues stay open and
	// undelivered, but are left out of remaining development effort and
	// of the stale, missing-estimate and over-estimate flags.
	AwaitingQAStatuses []string `json:"awaiting_qa_statuses"`
}

// DefaultRules are used until settings say otherwise. They are starting
// values, not agreed policy (Q12).
func DefaultRules(today time.Time) Rules {
	return Rules{
		Today:             today,
		StaleAfterDays:    14,
		MilestoneWarnDays: 14,
		HighPriorities:    []string{"High", "Urgent", "Immediate"},
		ReleaseKeywords:   []string{"QA", "UAT", "Release", "Handover"},
		// 67% of open in-scope issues were in this status at discovery
		// (P1-12); the CTO chose to count them separately.
		AwaitingQAStatuses: []string{"Fixed Not Tested"},
	}
}

func (r Rules) withDefaults() Rules {
	d := DefaultRules(r.Today)
	if r.StaleAfterDays <= 0 {
		r.StaleAfterDays = d.StaleAfterDays
	}
	if r.MilestoneWarnDays <= 0 {
		r.MilestoneWarnDays = d.MilestoneWarnDays
	}
	if r.HighPriorities == nil {
		r.HighPriorities = d.HighPriorities
	}
	if r.ReleaseKeywords == nil {
		r.ReleaseKeywords = d.ReleaseKeywords
	}
	if r.AwaitingQAStatuses == nil {
		r.AwaitingQAStatuses = d.AwaitingQAStatuses
	}
	return r
}

// AwaitingQA reports whether an open issue is waiting for testing.
func (r Rules) AwaitingQA(is Issue) bool {
	return !is.IsClosed && containsFold(r.withDefaults().AwaitingQAStatuses, is.StatusName)
}

// Severity orders flags. Critical and high affect project health; medium
// and low are data-quality signals that reduce forecast confidence.
type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
	Low      Severity = "low"
)

var rank = map[Severity]int{Critical: 0, High: 1, Medium: 2, Low: 3}

// Flag kinds.
const (
	KindBlocked          = "blocked_dependency"
	KindOverdue          = "overdue"
	KindStale            = "stale_activity"
	KindMissingAssignee  = "missing_owner"
	KindMissingEstimate  = "missing_estimate"
	KindMissingDueDate   = "missing_due_date"
	KindOverEstimate     = "over_estimate"
	KindMilestoneOverdue = "milestone_overdue"
	KindMilestoneAtRisk  = "milestone_at_risk"
	KindMilestoneNoDate  = "milestone_no_date"
	KindReleaseReadiness = "release_readiness"
	KindForecastLate     = "forecast_after_baseline"
	KindCapacity         = "capacity_risk"
	KindNoBaseline       = "no_baseline"
)

// Flag is one finding.
type Flag struct {
	Kind      string   `json:"kind"`
	Severity  Severity `json:"severity"`
	ProjectID int      `json:"project_id"`
	IssueID   *int     `json:"issue_id,omitempty"`
	VersionID *int     `json:"version_id,omitempty"`
	Title     string   `json:"title"`
	Evidence  string   `json:"evidence"`
	Action    string   `json:"action"`
}

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

func daysBetween(a, b time.Time) int { return int(day(b).Sub(day(a)).Hours() / 24) }

func ptr[T any](v T) *T { return &v }

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func mentions(keywords []string, s string) bool {
	low := strings.ToLower(s)
	for _, k := range keywords {
		if k != "" && strings.Contains(low, strings.ToLower(k)) {
			return true
		}
	}
	return false
}

// IssueFlags returns flags for open issues.
func IssueFlags(issues []Issue, relations []Relation, rules Rules) []Flag {
	rules = rules.withDefaults()
	byID := map[int]*Issue{}
	for i := range issues {
		byID[issues[i].ID] = &issues[i]
	}
	// blockers[x] lists open issues that block or precede x.
	blockers := map[int][]int{}
	for _, rel := range relations {
		if rel.Type != "blocks" && rel.Type != "precedes" {
			continue
		}
		from, ok := byID[rel.From]
		if !ok || from.IsClosed {
			continue
		}
		blockers[rel.To] = append(blockers[rel.To], rel.From)
	}

	var out []Flag
	for _, is := range issues {
		if is.IsClosed {
			continue
		}
		id := ptr(is.ID)
		qa := rules.AwaitingQA(is)
		add := func(kind string, sev Severity, title, evidence, action string) {
			out = append(out, Flag{Kind: kind, Severity: sev, ProjectID: is.ProjectID, IssueID: id, VersionID: is.VersionID,
				Title: title, Evidence: evidence, Action: action})
		}
		if bs := blockers[is.ID]; len(bs) > 0 {
			sort.Ints(bs)
			sev := High
			if containsFold(rules.HighPriorities, is.PriorityName) {
				sev = Critical
			}
			add(KindBlocked, sev, fmt.Sprintf("#%d is waiting on %d open issue(s)", is.ID, len(bs)),
				fmt.Sprintf("Blocked by %s; priority %s", issueList(bs), is.PriorityName),
				"Agree an owner and date for the blocking work, or remove the dependency if it no longer applies.")
		}
		if is.DueDate != nil && day(*is.DueDate).Before(day(rules.Today)) {
			n := daysBetween(*is.DueDate, rules.Today)
			add(KindOverdue, High, fmt.Sprintf("#%d is %d day(s) overdue", is.ID, n),
				fmt.Sprintf("Due %s, status %s, %s done", is.DueDate.Format(time.DateOnly), is.StatusName, pct(is.DoneRatio)),
				"Confirm a new date with the assignee or close it if the work is done.")
		}
		if n := daysBetween(is.UpdatedOn, rules.Today); !qa && n >= rules.StaleAfterDays {
			add(KindStale, Medium, fmt.Sprintf("#%d has had no update for %d days", is.ID, n),
				fmt.Sprintf("Last updated %s; threshold %d days", is.UpdatedOn.Format(time.DateOnly), rules.StaleAfterDays),
				"Ask the assignee for a status update.")
		}
		if is.AssigneeID == nil {
			add(KindMissingAssignee, Medium, fmt.Sprintf("#%d has no assignee", is.ID), "Assignee is empty in Redmine",
				"Assign an owner in Redmine.")
		}
		if is.Estimated == nil && !qa {
			add(KindMissingEstimate, Low, fmt.Sprintf("#%d has no estimate", is.ID), "Estimated time is empty in Redmine",
				"Add an estimate in Redmine so remaining effort can be calculated.")
		}
		if is.DueDate == nil && is.VersionID == nil {
			add(KindMissingDueDate, Low, fmt.Sprintf("#%d has no due date or target version", is.ID),
				"Neither due date nor target version is set", "Set a due date or a target version in Redmine.")
		}
		if !qa && is.Estimated != nil && is.Spent != nil && *is.Estimated > 0 && *is.Spent > *is.Estimated {
			add(KindOverEstimate, Low, fmt.Sprintf("#%d has used more time than estimated", is.ID),
				fmt.Sprintf("Spent %.1f h of %.1f h estimated", *is.Spent, *is.Estimated),
				"Re-estimate the remaining work.")
		}
	}
	return out
}

// MilestoneFlags returns flags for open versions.
func MilestoneFlags(versions []Version, issues []Issue, rules Rules) []Flag {
	rules = rules.withDefaults()
	type tally struct{ open, total int }
	t := map[int]*tally{}
	for _, is := range issues {
		if is.VersionID == nil {
			continue
		}
		c := t[*is.VersionID]
		if c == nil {
			c = &tally{}
			t[*is.VersionID] = c
		}
		c.total++
		if !is.IsClosed {
			c.open++
		}
	}
	var out []Flag
	for _, v := range versions {
		c := t[v.ID]
		if v.Status == "closed" || c == nil || c.open == 0 {
			continue
		}
		vid := ptr(v.ID)
		add := func(kind string, sev Severity, title, evidence, action string) {
			out = append(out, Flag{Kind: kind, Severity: sev, ProjectID: v.ProjectID, VersionID: vid, Title: title, Evidence: evidence, Action: action})
		}
		release := mentions(rules.ReleaseKeywords, v.Name)
		switch {
		case v.Due == nil:
			add(KindMilestoneNoDate, Low, fmt.Sprintf("Milestone %q has open work but no date", v.Name),
				fmt.Sprintf("%d of %d issues open", c.open, c.total), "Set the version's due date in Redmine.")
		case day(*v.Due).Before(day(rules.Today)):
			add(KindMilestoneOverdue, High, fmt.Sprintf("Milestone %q is past its date", v.Name),
				fmt.Sprintf("Due %s; %d of %d issues still open", v.Due.Format(time.DateOnly), c.open, c.total),
				"Agree a revised date or move the remaining issues.")
		case daysBetween(rules.Today, *v.Due) <= rules.MilestoneWarnDays:
			days := daysBetween(rules.Today, *v.Due)
			if release {
				add(KindReleaseReadiness, High, fmt.Sprintf("Release step %q is incomplete %d day(s) before its date", v.Name, days),
					fmt.Sprintf("Due %s; %d of %d issues open", v.Due.Format(time.DateOnly), c.open, c.total),
					"Check QA, UAT and release sign-off status with the project manager.")
			} else if c.open*2 > c.total {
				add(KindMilestoneAtRisk, High, fmt.Sprintf("Milestone %q is due in %d day(s) with most work open", v.Name, days),
					fmt.Sprintf("Due %s; %d of %d issues open", v.Due.Format(time.DateOnly), c.open, c.total),
					"Review scope or the date with the project manager.")
			}
		}
	}
	return out
}

// Sort orders flags by severity, then project, then issue.
func Sort(flags []Flag) {
	sort.SliceStable(flags, func(i, j int) bool {
		a, b := flags[i], flags[j]
		if rank[a.Severity] != rank[b.Severity] {
			return rank[a.Severity] < rank[b.Severity]
		}
		if a.ProjectID != b.ProjectID {
			return a.ProjectID < b.ProjectID
		}
		return deref(a.IssueID) < deref(b.IssueID)
	})
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// State is a project's delivery health.
type State string

const (
	OnTrack          State = "on_track"
	AtRisk           State = "at_risk"
	Blocked          State = "blocked"
	InsufficientData State = "insufficient_data"
)

// Health is a project's state with the reasons behind it.
type Health struct {
	State   State    `json:"state"`
	Reasons []string `json:"reasons"`
}

// ProjectHealth derives a state from a project's flags. hasSchedule is
// true when at least one open issue or open milestone has a date; without
// one there is nothing to judge progress against.
func ProjectHealth(openIssues int, hasSchedule bool, flags []Flag) Health {
	counts := map[string]int{}
	worst := Low
	for _, f := range flags {
		counts[f.Kind]++
		if rank[f.Severity] < rank[worst] {
			worst = f.Severity
		}
	}
	var reasons []string
	for _, k := range []string{KindBlocked, KindForecastLate, KindCapacity, KindReleaseReadiness, KindMilestoneOverdue, KindMilestoneAtRisk, KindOverdue} {
		if n := counts[k]; n > 0 {
			reasons = append(reasons, fmt.Sprintf("%d %s", n, strings.ReplaceAll(k, "_", " ")))
		}
	}
	switch {
	case worst == Critical:
		return Health{Blocked, reasons}
	case worst == High:
		return Health{AtRisk, reasons}
	case openIssues == 0:
		return Health{InsufficientData, []string{"no open issues in Redmine"}}
	case !hasSchedule:
		return Health{InsufficientData, []string{"no open issue or milestone has a date"}}
	}
	return Health{OnTrack, []string{}}
}

func issueList(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	return strings.Join(parts, ", ")
}

func pct(p *int) string {
	if p == nil {
		return "unknown %"
	}
	return fmt.Sprintf("%d%%", *p)
}
