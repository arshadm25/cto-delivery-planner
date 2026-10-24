// Package syncer copies Redmine data into the PostgreSQL mirror.
//
// A full run reads every in-scope project and marks rows that Redmine no
// longer returns as inactive. An incremental run reads issues updated since
// the last checkpoint, minus an overlap window, and then compares stored
// totals with the API's total_count per project.
//
// Each project is written in its own transaction, so a failure in one
// project leaves the previous data for it in place. The checkpoint only
// advances when every project committed and validated.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
	"github.com/arshadm25/cto-delivery-planner/internal/scope"
)

// Source is the read-only Redmine API. *redmine.Client implements it.
type Source interface {
	ListProjects(ctx context.Context) ([]redmine.Project, redmine.PageStats, error)
	ListIssueStatuses(ctx context.Context) ([]redmine.IssueStatus, error)
	ListVersions(ctx context.Context, projectID int) ([]redmine.Version, error)
	ListIssues(ctx context.Context, extra url.Values) ([]redmine.Issue, redmine.PageStats, error)
	CountIssues(ctx context.Context, extra url.Values) (int, error)
	ListTimeEntries(ctx context.Context, extra url.Values) ([]redmine.TimeEntry, redmine.PageStats, error)
}

// Kind is the type of sync run.
type Kind string

const (
	Full        Kind = "full"
	Incremental Kind = "incremental"
)

// CheckpointIssues is the sync_checkpoints row for issue updated_on.
const CheckpointIssues = "issues_updated_on"

// ErrAlreadyRunning means another process holds the sync lock.
var ErrAlreadyRunning = errors.New("syncer: another sync run is in progress")

// Options configure a Syncer.
type Options struct {
	Scope *scope.Scope
	// Overlap is subtracted from the checkpoint on incremental runs so
	// updates on the boundary are not missed.
	Overlap time.Duration
	// Concurrency bounds parallel per-project requests against Redmine.
	Concurrency int
	// TimeEntries enables time entry import on full runs.
	TimeEntries bool
	// Heartbeat is how often the run row is touched while running.
	Heartbeat time.Duration
	Logger    *slog.Logger
	Now       func() time.Time
}

// Syncer runs imports.
type Syncer struct {
	src  Source
	pool *pgxpool.Pool
	opt  Options
}

// New returns a Syncer. Scope is required.
func New(src Source, pool *pgxpool.Pool, opt Options) (*Syncer, error) {
	if opt.Scope == nil {
		return nil, errors.New("syncer: scope is required")
	}
	if opt.Concurrency <= 0 {
		opt.Concurrency = 2
	}
	if opt.Overlap < 0 {
		opt.Overlap = 0
	}
	if opt.Heartbeat <= 0 {
		opt.Heartbeat = 30 * time.Second
	}
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Syncer{src: src, pool: pool, opt: opt}, nil
}

// Counts are per-resource totals for one run.
type Counts struct {
	Fetched     int `json:"fetched"`
	Created     int `json:"created"`
	Updated     int `json:"updated"`
	Unchanged   int `json:"unchanged"`
	Inactivated int `json:"inactivated"`
	Skipped     int `json:"skipped"`
}

// Result describes a finished run.
type Result struct {
	RunID    int64
	Kind     Kind
	Outcome  string
	Counts   map[string]*Counts
	Warnings []string
	Errors   []string
	// NeedsFull is set when reconciliation found totals that differ, which
	// only a full run can repair (for example deleted issues).
	NeedsFull       bool
	CheckpointAfter *time.Time
}

type run struct {
	*Syncer
	res      *Result
	mu       sync.Mutex
	now      time.Time
	statuses map[int]bool // status ID -> is_closed
	// timeDenied lists projects whose time entries the account may not view.
	timeDenied []int
	maxUpdate  time.Time
}

func (r *run) count(resource string) *Counts {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.res.Counts[resource]
	if c == nil {
		c = &Counts{}
		r.res.Counts[resource] = c
	}
	return c
}

func (r *run) add(resource string, f func(c *Counts)) {
	c := r.count(resource)
	r.mu.Lock()
	f(c)
	r.mu.Unlock()
}

func (r *run) warn(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	r.mu.Lock()
	r.res.Warnings = append(r.res.Warnings, msg)
	r.mu.Unlock()
	r.opt.Logger.Warn(msg, "run_id", r.res.RunID)
}

func (r *run) fail(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	r.mu.Lock()
	r.res.Errors = append(r.res.Errors, msg)
	r.mu.Unlock()
	r.opt.Logger.Error(msg, "run_id", r.res.RunID)
}

// txCounts collects counts inside one transaction. They are added to the
// run only after the transaction commits, so a rolled-back batch is not
// reported as written.
type txCounts struct {
	counts    map[string]*Counts
	maxUpdate time.Time
}

func (tc *txCounts) add(resource string, f func(c *Counts)) {
	c := tc.counts[resource]
	if c == nil {
		c = &Counts{}
		tc.counts[resource] = c
	}
	f(c)
}

// inTx runs fn in a transaction, retrying once on a deadlock or
// serialization failure, and merges its counts after commit.
func (r *run) inTx(ctx context.Context, fn func(pgx.Tx, *txCounts) error) error {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		tc := &txCounts{counts: map[string]*Counts{}}
		err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return fn(tx, tc) })
		if err == nil {
			r.mu.Lock()
			for k, d := range tc.counts {
				c := r.res.Counts[k]
				if c == nil {
					c = &Counts{}
					r.res.Counts[k] = c
				}
				c.Fetched += d.Fetched
				c.Created += d.Created
				c.Updated += d.Updated
				c.Unchanged += d.Unchanged
				c.Inactivated += d.Inactivated
				c.Skipped += d.Skipped
			}
			if tc.maxUpdate.After(r.maxUpdate) {
				r.maxUpdate = tc.maxUpdate
			}
			r.mu.Unlock()
			return nil
		}
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || (pe.Code != "40P01" && pe.Code != "40001") {
			return err
		}
	}
	return err
}

const runLockID = 7_316_420_002

// Run performs one sync. An incremental run with no checkpoint becomes a
// full run. The returned error is set only when the run could not be
// recorded at all; failures inside the run are in Result.Errors.
func (s *Syncer) Run(ctx context.Context, kind Kind) (*Result, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, runLockID).Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, ErrAlreadyRunning
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, runLockID)

	// Holding the lock means no other run is alive, so any "running" row
	// belongs to a process that stopped. Resume is a fresh run: per-project
	// writes are idempotent and the checkpoint never advanced.
	if _, err := s.pool.Exec(ctx, `UPDATE sync_runs SET outcome = 'abandoned', finished_at = $1,
		error_summary = 'process stopped before the run finished' WHERE outcome = 'running'`, s.opt.Now()); err != nil {
		return nil, err
	}

	var checkpoint *time.Time
	err = s.pool.QueryRow(ctx, `SELECT value FROM sync_checkpoints WHERE name = $1`, CheckpointIssues).Scan(&checkpoint)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	r := &run{Syncer: s, now: s.opt.Now().UTC(), res: &Result{Kind: kind, Counts: map[string]*Counts{}}}
	if kind == Incremental && checkpoint == nil {
		kind = Full
		r.res.Kind = Full
	}
	if err := s.pool.QueryRow(ctx, `INSERT INTO sync_runs (kind, started_at, heartbeat_at, outcome, checkpoint_before)
		VALUES ($1, $2, $2, 'running', $3) RETURNING id`, kind, r.now, checkpoint).Scan(&r.res.RunID); err != nil {
		return nil, err
	}
	log := s.opt.Logger.With("run_id", r.res.RunID, "kind", kind)
	log.Info("sync started")

	hbCtx, stopHB := context.WithCancel(ctx)
	go r.heartbeat(hbCtx)

	r.execute(ctx, kind, checkpoint)
	stopHB()
	if len(r.timeDenied) > 0 {
		sort.Ints(r.timeDenied)
		r.warn("time entries skipped for %d project(s) the API key may not view (HTTP 403/404): %v", len(r.timeDenied), r.timeDenied)
	}

	switch {
	case ctx.Err() != nil:
		r.res.Outcome = "failed"
		r.fail("run cancelled: %v", ctx.Err())
	case len(r.res.Errors) == 0:
		r.res.Outcome = "succeeded"
	case r.anyProjectWritten():
		r.res.Outcome = "partial"
	default:
		r.res.Outcome = "failed"
	}

	if r.res.Outcome == "succeeded" {
		if cp, err := r.advanceCheckpoint(context.WithoutCancel(ctx), checkpoint); err != nil {
			r.res.Outcome = "failed"
			r.fail("checkpoint: %v", err)
		} else {
			r.res.CheckpointAfter = cp
		}
	}
	if err := r.finish(context.WithoutCancel(ctx)); err != nil {
		return r.res, err
	}
	log.Info("sync finished", "outcome", r.res.Outcome, "warnings", len(r.res.Warnings), "errors", len(r.res.Errors))
	return r.res, nil
}

func (r *run) anyProjectWritten() bool {
	c := r.res.Counts["projects_synced"]
	return c != nil && c.Created+c.Updated+c.Unchanged > 0
}

func (r *run) heartbeat(ctx context.Context) {
	t := time.NewTicker(r.opt.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.pool.Exec(ctx, `UPDATE sync_runs SET heartbeat_at = $1 WHERE id = $2`, r.opt.Now(), r.res.RunID)
		}
	}
}

func (r *run) execute(ctx context.Context, kind Kind, checkpoint *time.Time) {
	if err := r.syncStatuses(ctx); err != nil {
		r.fail("statuses: %v", err)
		return
	}
	visible, err := r.syncProjects(ctx)
	if err != nil {
		r.fail("projects: %v", err)
		return
	}
	var ids []int
	for _, id := range r.opt.Scope.ProjectIDs {
		if visible[id] {
			ids = append(ids, id)
		} else {
			r.warn("in-scope project %d is not visible to the API key", id)
		}
	}

	if kind == Full {
		r.forEach(ctx, ids, func(ctx context.Context, id int) error { return r.fullProject(ctx, id) })
		return
	}
	r.forEach(ctx, ids, func(ctx context.Context, id int) error { return r.syncVersions(ctx, id) })
	since := checkpoint.Add(-r.opt.Overlap)
	if err := r.incrementalIssues(ctx, since); err != nil {
		r.fail("incremental issues: %v", err)
		return
	}
	r.reconcileCounts(ctx, ids)
}

// forEach runs fn for each project with bounded concurrency. A project's
// error is recorded and does not stop the others.
func (r *run) forEach(ctx context.Context, ids []int, fn func(context.Context, int) error) {
	sem := make(chan struct{}, r.opt.Concurrency)
	var wg sync.WaitGroup
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(ctx, id); err != nil {
				r.fail("project %d: %v", id, err)
			}
		}(id)
	}
	wg.Wait()
}

func (r *run) syncStatuses(ctx context.Context) error {
	sts, err := r.src.ListIssueStatuses(ctx)
	if err != nil {
		return err
	}
	if len(sts) == 0 {
		return errors.New("API returned no issue statuses")
	}
	r.statuses = map[int]bool{}
	return r.inTx(ctx, func(tx pgx.Tx, tc *txCounts) error {
		for _, st := range sts {
			r.statuses[st.ID] = st.IsClosed
			if _, err := tx.Exec(ctx, `INSERT INTO redmine_statuses (redmine_id, name, is_closed, last_synced_at)
				VALUES ($1, $2, $3, $4) ON CONFLICT (redmine_id) DO UPDATE
				SET name = EXCLUDED.name, is_closed = EXCLUDED.is_closed, last_synced_at = EXCLUDED.last_synced_at`,
				st.ID, st.Name, st.IsClosed, r.now); err != nil {
				return err
			}
		}
		tc.add("statuses", func(c *Counts) { c.Fetched += len(sts) })
		return nil
	})
}

// syncProjects upserts every visible project and returns the visible set.
func (r *run) syncProjects(ctx context.Context) (map[int]bool, error) {
	ps, stats, err := r.src.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkComplete(len(ps), stats); err != nil {
		return nil, err
	}
	visible := map[int]bool{}
	err = r.inTx(ctx, func(tx pgx.Tx, tc *txCounts) error {
		var ids []int32
		for _, p := range ps {
			visible[p.ID] = true
			ids = append(ids, int32(p.ID))
			var parent *int
			if p.Parent != nil {
				parent = &p.Parent.ID
			}
			created, err := parseTimePtr(&p.CreatedOn)
			if err != nil {
				return fmt.Errorf("project %d created_on: %w", p.ID, err)
			}
			updated, err := parseTimePtr(&p.UpdatedOn)
			if err != nil {
				return fmt.Errorf("project %d updated_on: %w", p.ID, err)
			}
			var inserted bool
			if err := tx.QueryRow(ctx, `INSERT INTO redmine_projects (redmine_id, name, identifier, status, is_public,
					parent_redmine_id, created_on, updated_on, in_scope, last_synced_at, inactive_since)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL)
				ON CONFLICT (redmine_id) DO UPDATE SET name = EXCLUDED.name, identifier = EXCLUDED.identifier,
					status = EXCLUDED.status, is_public = EXCLUDED.is_public, parent_redmine_id = EXCLUDED.parent_redmine_id,
					created_on = EXCLUDED.created_on, updated_on = EXCLUDED.updated_on, in_scope = EXCLUDED.in_scope,
					last_synced_at = EXCLUDED.last_synced_at, inactive_since = NULL
				RETURNING (xmax = 0)`,
				p.ID, p.Name, p.Identifier, p.Status, p.IsPublic, parent, created, updated,
				r.opt.Scope.Includes(p.ID), r.now).Scan(&inserted); err != nil {
				return err
			}
			tc.add("projects", func(c *Counts) {
				c.Fetched++
				if inserted {
					c.Created++
				} else {
					c.Updated++
				}
			})
		}
		tag, err := tx.Exec(ctx, `UPDATE redmine_projects SET inactive_since = $1, in_scope = false
			WHERE inactive_since IS NULL AND NOT (redmine_id = ANY($2))`, r.now, ids)
		if err != nil {
			return err
		}
		tc.add("projects", func(c *Counts) { c.Inactivated += int(tag.RowsAffected()) })
		return nil
	})
	return visible, err
}

func (r *run) syncVersions(ctx context.Context, projectID int) error {
	vs, err := r.src.ListVersions(ctx, projectID)
	if err != nil {
		return fmt.Errorf("versions: %w", err)
	}
	return r.inTx(ctx, func(tx pgx.Tx, tc *txCounts) error { return r.writeVersions(ctx, tx, tc, projectID, vs) })
}

func (r *run) writeVersions(ctx context.Context, tx pgx.Tx, tc *txCounts, projectID int, vs []redmine.Version) error {
	sort.Slice(vs, func(i, j int) bool { return vs[i].ID < vs[j].ID })
	var own []int32
	for _, v := range vs {
		if v.Project.ID == projectID {
			own = append(own, int32(v.ID))
		}
		due, err := parseDatePtr(v.EffectiveDate)
		if err != nil {
			return fmt.Errorf("version %d due_date: %w", v.ID, err)
		}
		created, err := parseTimePtr(&v.CreatedOn)
		if err != nil {
			return fmt.Errorf("version %d created_on: %w", v.ID, err)
		}
		updated, err := parseTimePtr(&v.UpdatedOn)
		if err != nil {
			return fmt.Errorf("version %d updated_on: %w", v.ID, err)
		}
		var inserted bool
		if err := tx.QueryRow(ctx, `INSERT INTO redmine_versions (redmine_id, project_redmine_id, name, status, sharing,
				effective_date, created_on, updated_on, last_synced_at, inactive_since)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
			ON CONFLICT (redmine_id) DO UPDATE SET project_redmine_id = EXCLUDED.project_redmine_id, name = EXCLUDED.name,
				status = EXCLUDED.status, sharing = EXCLUDED.sharing, effective_date = EXCLUDED.effective_date,
				created_on = EXCLUDED.created_on, updated_on = EXCLUDED.updated_on,
				last_synced_at = EXCLUDED.last_synced_at, inactive_since = NULL
			RETURNING (xmax = 0)`,
			v.ID, v.Project.ID, v.Name, v.Status, nullString(v.Sharing), due, created, updated, r.now).Scan(&inserted); err != nil {
			return err
		}
		tc.add("versions", func(c *Counts) {
			c.Fetched++
			if inserted {
				c.Created++
			} else {
				c.Updated++
			}
		})
	}
	tag, err := tx.Exec(ctx, `UPDATE redmine_versions SET inactive_since = $1
		WHERE project_redmine_id = $2 AND inactive_since IS NULL AND NOT (redmine_id = ANY($3))`, r.now, projectID, own)
	if err != nil {
		return err
	}
	tc.add("versions", func(c *Counts) { c.Inactivated += int(tag.RowsAffected()) })
	return nil
}

// projectIssueQuery limits a query to one project without its subprojects,
// because every subproject is listed in scope on its own.
func projectIssueQuery(projectID int) url.Values {
	return url.Values{"project_id": {strconv.Itoa(projectID)}, "subproject_id": {"!*"}}
}

// fetchIssues fetches with one retry when the result is incomplete, which
// happens when issues are deleted or moved during offset pagination.
func (r *run) fetchIssues(ctx context.Context, q url.Values) ([]redmine.Issue, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		issues, stats, err := r.src.ListIssues(ctx, q)
		if err != nil {
			return nil, err
		}
		issues = uniqueIssues(issues)
		if lastErr = checkComplete(len(issues), stats); lastErr == nil {
			return issues, nil
		}
	}
	return nil, lastErr
}

func (r *run) fullProject(ctx context.Context, projectID int) error {
	vs, err := r.src.ListVersions(ctx, projectID)
	if err != nil {
		return fmt.Errorf("versions: %w", err)
	}
	q := projectIssueQuery(projectID)
	q.Set("include", "relations")
	issues, err := r.fetchIssues(ctx, q)
	if err != nil {
		return fmt.Errorf("issues: %w", err)
	}
	for _, is := range issues {
		if is.Project.ID != projectID {
			return fmt.Errorf("issue %d belongs to project %d; subproject filter was not applied", is.ID, is.Project.ID)
		}
	}
	entries, err := r.fetchTimeEntries(ctx, projectID)
	if err != nil {
		return fmt.Errorf("time entries: %w", err)
	}

	err = r.inTx(ctx, func(tx pgx.Tx, tc *txCounts) error {
		if err := r.upsertUsers(ctx, tx, issues, entries); err != nil {
			return err
		}
		if err := r.writeVersions(ctx, tx, tc, projectID, vs); err != nil {
			return err
		}
		if err := r.writeIssues(ctx, tx, tc, issues); err != nil {
			return err
		}
		var seen []int32
		for _, is := range issues {
			seen = append(seen, int32(is.ID))
		}
		tag, err := tx.Exec(ctx, `UPDATE redmine_issues SET inactive_since = $1
			WHERE project_redmine_id = $2 AND inactive_since IS NULL AND NOT (redmine_id = ANY($3))`, r.now, projectID, seen)
		if err != nil {
			return err
		}
		tc.add("issues", func(c *Counts) { c.Inactivated += int(tag.RowsAffected()) })
		if entries != nil {
			if err := r.writeTimeEntries(ctx, tx, tc, projectID, entries); err != nil {
				return err
			}
		}
		var stored int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM redmine_issues
			WHERE project_redmine_id = $1 AND inactive_since IS NULL`, projectID).Scan(&stored); err != nil {
			return err
		}
		// Validation before commit: the stored active set must equal what
		// the API returned for this project.
		if stored != len(issues) {
			return fmt.Errorf("validation: %d active issues stored, API returned %d", stored, len(issues))
		}
		_, err = tx.Exec(ctx, `INSERT INTO sync_reconciliation (run_id, project_redmine_id, api_total, stored_active, matches)
			VALUES ($1, $2, $3, $4, true)`, r.res.RunID, projectID, len(issues), stored)
		return err
	})
	if err == nil {
		r.add("projects_synced", func(c *Counts) { c.Updated++ })
	}
	return err
}

func (r *run) fetchTimeEntries(ctx context.Context, projectID int) ([]redmine.TimeEntry, error) {
	if !r.opt.TimeEntries {
		return nil, nil
	}
	es, stats, err := r.src.ListTimeEntries(ctx, url.Values{"project_id": {strconv.Itoa(projectID)}})
	var se *redmine.StatusError
	if errors.As(err, &se) && (se.Status == http.StatusForbidden || se.Status == http.StatusNotFound) {
		// Usually the time tracking module is off for this project or the
		// account's role there lacks "View spent time". Other projects
		// are still read; the run reports every project skipped.
		r.mu.Lock()
		r.timeDenied = append(r.timeDenied, projectID)
		r.mu.Unlock()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := checkComplete(len(es), stats); err != nil {
		return nil, err
	}
	// The endpoint includes subprojects; keep this project's entries only.
	out := es[:0]
	for _, e := range es {
		if e.Project.ID == projectID {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []redmine.TimeEntry{}
	}
	return out, nil
}

func (r *run) writeTimeEntries(ctx context.Context, tx pgx.Tx, tc *txCounts, projectID int, es []redmine.TimeEntry) error {
	sort.Slice(es, func(i, j int) bool { return es[i].ID < es[j].ID })
	var seen []int32
	for _, e := range es {
		seen = append(seen, int32(e.ID))
		spent, err := parseDate(e.SpentOn)
		if err != nil {
			return fmt.Errorf("time entry %d spent_on: %w", e.ID, err)
		}
		updated, err := parseTimePtr(&e.UpdatedOn)
		if err != nil {
			return fmt.Errorf("time entry %d updated_on: %w", e.ID, err)
		}
		var issue *int
		if e.Issue != nil {
			issue = &e.Issue.ID
		}
		var inserted bool
		if err := tx.QueryRow(ctx, `INSERT INTO redmine_time_entries (redmine_id, project_redmine_id, issue_redmine_id,
				user_redmine_id, activity, hours, spent_on, updated_on, last_synced_at, inactive_since)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
			ON CONFLICT (redmine_id) DO UPDATE SET project_redmine_id = EXCLUDED.project_redmine_id,
				issue_redmine_id = EXCLUDED.issue_redmine_id, user_redmine_id = EXCLUDED.user_redmine_id,
				activity = EXCLUDED.activity, hours = EXCLUDED.hours, spent_on = EXCLUDED.spent_on,
				updated_on = EXCLUDED.updated_on, last_synced_at = EXCLUDED.last_synced_at, inactive_since = NULL
			RETURNING (xmax = 0)`,
			e.ID, projectID, issue, e.User.ID, e.Activity.Name, e.Hours, spent, updated, r.now).Scan(&inserted); err != nil {
			return err
		}
		tc.add("time_entries", func(c *Counts) { tally(c, inserted) })
	}
	tag, err := tx.Exec(ctx, `UPDATE redmine_time_entries SET inactive_since = $1
		WHERE project_redmine_id = $2 AND inactive_since IS NULL AND NOT (redmine_id = ANY($3))`, r.now, projectID, seen)
	if err != nil {
		return err
	}
	tc.add("time_entries", func(c *Counts) { c.Inactivated += int(tag.RowsAffected()) })
	return nil
}

// upsertUsers stores the ID and display name of everyone referenced. Rows
// are written in ID order, and every transaction writes users, versions,
// issues, relations and time entries in that order, so concurrent project
// transactions cannot deadlock on shared rows.
func (r *run) upsertUsers(ctx context.Context, tx pgx.Tx, issues []redmine.Issue, entries []redmine.TimeEntry) error {
	names := map[int]string{}
	add := func(u *redmine.IDName) {
		if u != nil && u.ID > 0 {
			names[u.ID] = u.Name
		}
	}
	for i := range issues {
		add(issues[i].Author)
		add(issues[i].AssignedTo)
	}
	for i := range entries {
		add(&entries[i].User)
	}
	ids := make([]int, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO redmine_users (redmine_id, name, last_synced_at) VALUES ($1, $2, $3)
			ON CONFLICT (redmine_id) DO UPDATE SET name = EXCLUDED.name, last_synced_at = EXCLUDED.last_synced_at`,
			id, names[id], r.now); err != nil {
			return err
		}
	}
	return nil
}

// writeIssues upserts issues and replaces their relations.
func (r *run) writeIssues(ctx context.Context, tx pgx.Tx, tc *txCounts, issues []redmine.Issue) error {
	sort.Slice(issues, func(i, j int) bool { return issues[i].ID < issues[j].ID })
	if len(issues) == 0 {
		return nil
	}
	ids := make([]int32, len(issues))
	for i, is := range issues {
		ids[i] = int32(is.ID)
	}
	rows, err := tx.Query(ctx, `SELECT redmine_id, source_hash, inactive_since IS NOT NULL FROM redmine_issues WHERE redmine_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	type prior struct {
		hash     string
		inactive bool
	}
	before := map[int]prior{}
	for rows.Next() {
		var id int
		var p prior
		if err := rows.Scan(&id, &p.hash, &p.inactive); err != nil {
			rows.Close()
			return err
		}
		before[id] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var relIDs []int32
	rels := map[int]redmine.Relation{}
	for _, is := range issues {
		row, err := toIssueRow(is)
		if err != nil {
			return fmt.Errorf("issue %d: %w", is.ID, err)
		}
		if row.updatedOn.After(tc.maxUpdate) {
			tc.maxUpdate = row.updatedOn
		}
		if _, err := tx.Exec(ctx, `INSERT INTO redmine_issues (redmine_id, project_redmine_id, parent_redmine_id, subject,
				tracker_id, tracker_name, status_id, status_name, priority_id, priority_name, author_redmine_id,
				assigned_to_redmine_id, fixed_version_redmine_id, start_date, due_date, done_ratio, estimated_hours,
				spent_hours, is_private, created_on, updated_on, closed_on, source_hash, first_synced_at, last_synced_at,
				inactive_since)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $24, NULL)
			ON CONFLICT (redmine_id) DO UPDATE SET project_redmine_id = EXCLUDED.project_redmine_id,
				parent_redmine_id = EXCLUDED.parent_redmine_id, subject = EXCLUDED.subject,
				tracker_id = EXCLUDED.tracker_id, tracker_name = EXCLUDED.tracker_name,
				status_id = EXCLUDED.status_id, status_name = EXCLUDED.status_name,
				priority_id = EXCLUDED.priority_id, priority_name = EXCLUDED.priority_name,
				author_redmine_id = EXCLUDED.author_redmine_id, assigned_to_redmine_id = EXCLUDED.assigned_to_redmine_id,
				fixed_version_redmine_id = EXCLUDED.fixed_version_redmine_id, start_date = EXCLUDED.start_date,
				due_date = EXCLUDED.due_date, done_ratio = EXCLUDED.done_ratio, estimated_hours = EXCLUDED.estimated_hours,
				spent_hours = EXCLUDED.spent_hours, is_private = EXCLUDED.is_private, created_on = EXCLUDED.created_on,
				updated_on = EXCLUDED.updated_on, closed_on = EXCLUDED.closed_on, source_hash = EXCLUDED.source_hash,
				last_synced_at = EXCLUDED.last_synced_at, inactive_since = NULL`,
			row.args(r.now)...); err != nil {
			return fmt.Errorf("issue %d: %w", is.ID, err)
		}
		p, existed := before[is.ID]
		tc.add("issues", func(c *Counts) {
			c.Fetched++
			switch {
			case !existed:
				c.Created++
			case p.hash != row.hash || p.inactive:
				c.Updated++
			default:
				c.Unchanged++
			}
		})

		for _, rel := range is.Relations {
			rels[rel.ID] = rel
		}
	}
	relKeys := make([]int, 0, len(rels))
	for id := range rels {
		relKeys = append(relKeys, id)
	}
	sort.Ints(relKeys)
	for _, id := range relKeys {
		rel := rels[id]
		relIDs = append(relIDs, int32(rel.ID))
		var inserted bool
		if err := tx.QueryRow(ctx, `INSERT INTO redmine_issue_relations (redmine_id, issue_from_redmine_id,
				issue_to_redmine_id, relation_type, delay, last_synced_at)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (redmine_id) DO UPDATE SET
				issue_from_redmine_id = EXCLUDED.issue_from_redmine_id, issue_to_redmine_id = EXCLUDED.issue_to_redmine_id,
				relation_type = EXCLUDED.relation_type, delay = EXCLUDED.delay, last_synced_at = EXCLUDED.last_synced_at
			RETURNING (xmax = 0)`,
			rel.ID, rel.IssueID, rel.IssueToID, rel.RelationType, rel.Delay, r.now).Scan(&inserted); err != nil {
			return fmt.Errorf("relation %d: %w", rel.ID, err)
		}
		tc.add("relations", func(c *Counts) { tally(c, inserted) })
	}
	// Each issue returns every relation it takes part in, so any stored
	// relation touching these issues that was not returned is gone.
	tag, err := tx.Exec(ctx, `DELETE FROM redmine_issue_relations
		WHERE (issue_from_redmine_id = ANY($1) OR issue_to_redmine_id = ANY($1)) AND NOT (redmine_id = ANY($2))`, ids, relIDs)
	if err != nil {
		return err
	}
	tc.add("relations", func(c *Counts) { c.Inactivated += int(tag.RowsAffected()) })
	return nil
}

func (r *run) incrementalIssues(ctx context.Context, since time.Time) error {
	q := url.Values{
		"updated_on": {">=" + since.UTC().Format(time.RFC3339)},
		"include":    {"relations"},
	}
	issues, err := r.fetchIssues(ctx, q)
	if err != nil {
		return err
	}
	// Keep issues in scope, plus stored issues that moved out of scope so
	// their project changes in the mirror.
	ids := make([]int32, len(issues))
	for i, is := range issues {
		ids[i] = int32(is.ID)
	}
	rows, err := r.pool.Query(ctx, `SELECT redmine_id FROM redmine_issues WHERE redmine_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	known, err := pgx.CollectRows(rows, pgx.RowTo[int32])
	if err != nil {
		return err
	}
	stored := map[int]bool{}
	for _, id := range known {
		stored[int(id)] = true
	}
	byProject := map[int][]redmine.Issue{}
	for _, is := range issues {
		if r.opt.Scope.Includes(is.Project.ID) || stored[is.ID] {
			byProject[is.Project.ID] = append(byProject[is.Project.ID], is)
		} else {
			r.add("issues", func(c *Counts) { c.Skipped++ })
		}
	}
	pids := make([]int, 0, len(byProject))
	for id := range byProject {
		pids = append(pids, id)
	}
	sort.Ints(pids)
	for _, pid := range pids {
		err := r.inTx(ctx, func(tx pgx.Tx, tc *txCounts) error {
			if err := r.upsertUsers(ctx, tx, byProject[pid], nil); err != nil {
				return err
			}
			return r.writeIssues(ctx, tx, tc, byProject[pid])
		})
		if err != nil {
			r.fail("project %d: issues: %v", pid, err)
			continue
		}
		r.add("projects_synced", func(c *Counts) { c.Updated++ })
	}
	if len(pids) == 0 {
		// Nothing changed; record that the run still covered the scope.
		r.add("projects_synced", func(c *Counts) { c.Unchanged++ })
	}
	return nil
}

// reconcileCounts compares stored active issues per project with the API.
// A mismatch means issues were deleted, moved or hidden, which an
// incremental run cannot see; the run asks for a full run instead.
func (r *run) reconcileCounts(ctx context.Context, ids []int) {
	r.forEach(ctx, ids, func(ctx context.Context, id int) error {
		total, err := r.src.CountIssues(ctx, projectIssueQuery(id))
		if err != nil {
			return fmt.Errorf("reconcile: %w", err)
		}
		var stored int
		if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM redmine_issues WHERE project_redmine_id = $1
			AND inactive_since IS NULL`, id).Scan(&stored); err != nil {
			return err
		}
		ok := total == stored
		var note *string
		if !ok {
			n := "totals differ; a full run will mark missing issues inactive"
			note = &n
			r.mu.Lock()
			r.res.NeedsFull = true
			r.mu.Unlock()
			r.warn("project %d: API reports %d issues, %d stored", id, total, stored)
		}
		_, err = r.pool.Exec(ctx, `INSERT INTO sync_reconciliation (run_id, project_redmine_id, api_total, stored_active, matches, note)
			VALUES ($1, $2, $3, $4, $5, $6)`, r.res.RunID, id, total, stored, ok, note)
		return err
	})
}

// advanceCheckpoint moves the checkpoint to the newest updated_on seen, as
// reported by Redmine's own clock. It never moves backwards.
func (r *run) advanceCheckpoint(ctx context.Context, before *time.Time) (*time.Time, error) {
	next := r.maxUpdate
	if before != nil && before.After(next) {
		next = *before
	}
	if next.IsZero() {
		// No issues at all yet; use the run start so incremental runs can begin.
		next = r.now
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO sync_checkpoints (name, value, run_id, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, run_id = EXCLUDED.run_id, updated_at = EXCLUDED.updated_at`,
		CheckpointIssues, next, r.res.RunID, r.opt.Now())
	return &next, err
}

func (r *run) finish(ctx context.Context) error {
	var summary *string
	if len(r.res.Errors) > 0 {
		s := r.res.Errors[0]
		if n := len(r.res.Errors); n > 1 {
			s = fmt.Sprintf("%s (and %d more)", s, n-1)
		}
		summary = &s
	}
	// warnings holds warnings followed by errors, so the sync status
	// screen can show both from one column.
	notes := append([]string{}, r.res.Warnings...)
	notes = append(notes, prefixed("error: ", r.res.Errors)...)
	_, err := r.pool.Exec(ctx, `UPDATE sync_runs SET finished_at = $1, outcome = $2, counts = $3, warnings = $4,
		checkpoint_after = $5, error_summary = $6 WHERE id = $7`,
		r.opt.Now(), r.res.Outcome, r.res.Counts, notes, r.res.CheckpointAfter, summary, r.res.RunID)
	return err
}

func prefixed(p string, ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = p + s
	}
	return out
}

func checkComplete(got int, stats redmine.PageStats) error {
	if got != stats.FinalTotal {
		return fmt.Errorf("incomplete fetch: got %d unique records, API total_count %d (data changed during pagination)", got, stats.FinalTotal)
	}
	return nil
}

func uniqueIssues(in []redmine.Issue) []redmine.Issue {
	seen := map[int]bool{}
	out := in[:0]
	for _, is := range in {
		if seen[is.ID] {
			continue
		}
		seen[is.ID] = true
		out = append(out, is)
	}
	return out
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// tally counts one fetched row as created or updated.
func tally(c *Counts, inserted bool) {
	c.Fetched++
	if inserted {
		c.Created++
	} else {
		c.Updated++
	}
}
