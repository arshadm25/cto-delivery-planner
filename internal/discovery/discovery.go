// Package discovery builds the Phase 1 baseline: which projects and issues the
// Redmine service account can see, how they are counted, and where data is
// missing. It only reads from Redmine.
package discovery

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
)

// Source is the part of the Redmine client discovery needs.
type Source interface {
	CurrentUser(ctx context.Context) (redmine.User, error)
	ListProjects(ctx context.Context) ([]redmine.Project, redmine.PageStats, error)
	ListIssueStatuses(ctx context.Context) ([]redmine.IssueStatus, error)
	ListIssues(ctx context.Context, extra url.Values) ([]redmine.Issue, redmine.PageStats, error)
}

// ProjectCount is one project row in the baseline.
type ProjectCount struct {
	Project redmine.Project
	Depth   int
	Total   int
	Open    int
	Closed  int
}

// Gaps counts issues with missing planning data. Counts cover open and
// closed issues together; OpenX fields cover open issues only.
type Gaps struct {
	NoAssignee, OpenNoAssignee int
	NoEstimate, OpenNoEstimate int
	NoDueDate, OpenNoDueDate   int
	NoVersion, OpenNoVersion   int
}

// Result is everything one discovery run found.
type Result struct {
	GeneratedAt time.Time
	User        redmine.User

	Projects     []ProjectCount // in tree order
	ProjectStats redmine.PageStats

	Issues      []redmine.Issue // unique by ID, sorted by ID
	IssueStats  redmine.PageStats
	StatusKnown bool // false if /issue_statuses.json could not be read
	OpenTotal   int
	ClosedTotal int

	// Issues whose project is not in the visible project list.
	OrphanProjectIDs map[int]int
	// Parent IDs referenced by issues but not returned by the API.
	MissingParentIDs []int
	// Status IDs on issues that were not in /issue_statuses.json.
	UnknownStatusIDs []int

	Gaps     Gaps
	Warnings []string
	closed   map[int]bool
}

// IsClosed reports whether the issue's status is a closed status.
// It returns false when statuses could not be read; check StatusKnown.
func (r *Result) IsClosed(is redmine.Issue) bool { return r.closed[is.Status.ID] }

// Run performs a full read-only discovery.
func Run(ctx context.Context, src Source, now time.Time) (*Result, error) {
	r := &Result{GeneratedAt: now.UTC(), OrphanProjectIDs: map[int]int{}, closed: map[int]bool{}}

	user, err := src.CurrentUser(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication check failed: %w", err)
	}
	r.User = user
	if user.Admin {
		r.warn("The API key belongs to an administrator. The plan calls for a dedicated non-admin read-only account, so counts here may include projects the service account will not see.")
	}

	projects, pstats, err := src.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}
	r.ProjectStats = pstats
	projects = uniqueProjects(projects)

	statuses, err := src.ListIssueStatuses(ctx)
	if err != nil {
		r.warn(fmt.Sprintf("Could not read issue statuses (%v), so open and closed counts are unknown.", err))
	} else {
		r.StatusKnown = true
		for _, s := range statuses {
			r.closed[s.ID] = s.IsClosed
		}
	}

	issues, istats, err := src.ListIssues(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing issues: %w", err)
	}
	r.IssueStats = istats
	r.Issues = uniqueIssues(issues)

	r.countIssues(projects)
	r.checkPagination()
	return r, nil
}

func (r *Result) warn(s string) { r.Warnings = append(r.Warnings, s) }

func (r *Result) countIssues(projects []redmine.Project) {
	byProject := map[int]*ProjectCount{}
	for _, p := range projects {
		byProject[p.ID] = &ProjectCount{Project: p}
	}
	ids := map[int]bool{}
	for _, is := range r.Issues {
		ids[is.ID] = true
	}
	missingParents := map[int]bool{}
	unknownStatus := map[int]bool{}

	for _, is := range r.Issues {
		closed := r.IsClosed(is)
		if r.StatusKnown {
			if _, ok := r.closed[is.Status.ID]; !ok {
				unknownStatus[is.Status.ID] = true
			}
			if closed {
				r.ClosedTotal++
			} else {
				r.OpenTotal++
			}
		}

		if pc, ok := byProject[is.Project.ID]; ok {
			pc.Total++
			if r.StatusKnown {
				if closed {
					pc.Closed++
				} else {
					pc.Open++
				}
			}
		} else {
			r.OrphanProjectIDs[is.Project.ID]++
		}
		if is.Parent != nil && !ids[is.Parent.ID] {
			missingParents[is.Parent.ID] = true
		}

		open := r.StatusKnown && !closed
		if is.AssignedTo == nil {
			r.Gaps.NoAssignee++
			if open {
				r.Gaps.OpenNoAssignee++
			}
		}
		if is.EstimatedHours == nil {
			r.Gaps.NoEstimate++
			if open {
				r.Gaps.OpenNoEstimate++
			}
		}
		if is.DueDate == nil {
			r.Gaps.NoDueDate++
			if open {
				r.Gaps.OpenNoDueDate++
			}
		}
		if is.FixedVersion == nil {
			r.Gaps.NoVersion++
			if open {
				r.Gaps.OpenNoVersion++
			}
		}
	}

	r.Projects = treeOrder(projects, byProject)
	r.MissingParentIDs = sortedKeys(missingParents)
	r.UnknownStatusIDs = sortedKeys(unknownStatus)

	if len(r.OrphanProjectIDs) > 0 {
		r.warn("Some issues belong to projects missing from the project list. The project listing may hide closed or archived projects; check them in Administration › Projects.")
	}
	if len(r.MissingParentIDs) > 0 {
		r.warn(fmt.Sprintf("%d parent issue IDs are referenced but were not returned, likely because the service account cannot see them.", len(r.MissingParentIDs)))
	}
}

func (r *Result) checkPagination() {
	check := func(name string, s redmine.PageStats, unique int) {
		if s.ReportedTotal != s.FinalTotal {
			r.warn(fmt.Sprintf("%s total changed during the run (%d at start, %d at end). Re-run when Redmine is quiet.", name, s.ReportedTotal, s.FinalTotal))
		}
		if s.Duplicates > 0 {
			r.warn(fmt.Sprintf("%d %s were returned on more than one page and were de-duplicated.", s.Duplicates, name))
		}
		if unique != s.FinalTotal {
			r.warn(fmt.Sprintf("Fetched %d unique %s but the API reported %d. Re-run before accepting the baseline.", unique, name, s.FinalTotal))
		}
	}
	check("projects", r.ProjectStats, len(r.Projects))
	check("issues", r.IssueStats, len(r.Issues))
}

// uniqueIssues keeps the last copy of each ID (the most recently fetched)
// and sorts by ID.
func uniqueIssues(in []redmine.Issue) []redmine.Issue {
	byID := map[int]redmine.Issue{}
	for _, is := range in {
		byID[is.ID] = is
	}
	out := make([]redmine.Issue, 0, len(byID))
	for _, is := range byID {
		out = append(out, is)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func uniqueProjects(in []redmine.Project) []redmine.Project {
	byID := map[int]redmine.Project{}
	for _, p := range in {
		byID[p.ID] = p
	}
	out := make([]redmine.Project, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// treeOrder lists projects parent-first, children sorted by name. Projects
// whose parent is not visible are treated as roots.
func treeOrder(projects []redmine.Project, counts map[int]*ProjectCount) []ProjectCount {
	children := map[int][]redmine.Project{}
	var roots []redmine.Project
	for _, p := range projects {
		if p.Parent != nil {
			if _, ok := counts[p.Parent.ID]; ok {
				children[p.Parent.ID] = append(children[p.Parent.ID], p)
				continue
			}
		}
		roots = append(roots, p)
	}
	byName := func(ps []redmine.Project) {
		sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
	}
	var out []ProjectCount
	var walk func(p redmine.Project, depth int)
	walk = func(p redmine.Project, depth int) {
		pc := *counts[p.ID]
		pc.Depth = depth
		out = append(out, pc)
		kids := children[p.ID]
		byName(kids)
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	byName(roots)
	for _, p := range roots {
		walk(p, 0)
	}
	return out
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
