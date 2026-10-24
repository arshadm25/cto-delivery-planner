// Package store reads the mirror for the API. Data is loaded per sync run
// into an in-memory Dataset, because every view needs the same few
// thousand in-scope issues and the rules run over all of them.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arshadm25/cto-delivery-planner/internal/audit"
	"github.com/arshadm25/cto-delivery-planner/internal/risks"
)

// Project is an in-scope project with its planner-owned roles.
type Project struct {
	ID              int        `json:"id"`
	Name            string     `json:"name"`
	Identifier      string     `json:"identifier"`
	ParentID        *int       `json:"parent_id"`
	Status          int        `json:"status"`
	LastSyncedAt    time.Time  `json:"last_synced_at"`
	Owner           *string    `json:"owner"`
	TechLead        *string    `json:"tech_lead"`
	RolesVerifiedAt *time.Time `json:"roles_verified_at"`
}

// Dataset is one consistent read of the in-scope mirror.
type Dataset struct {
	LoadedAt  time.Time
	RunID     int64
	Projects  []Project
	Issues    []risks.Issue
	Relations []risks.Relation
	Versions  []risks.Version
	// Index helpers.
	ProjectByID     map[int]*Project
	IssuesByProject map[int][]int // indexes into Issues
	VersionByID     map[int]*risks.Version
}

// Store wraps the pool and caches the dataset until the next sync run or
// planner-side change.
type Store struct {
	Pool *pgxpool.Pool

	mu      sync.Mutex
	cached  *Dataset
	version int64 // bumped by Invalidate
	cachedV int64
}

// New returns a Store.
func New(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

// Invalidate drops the cache after a planner-side write.
func (s *Store) Invalidate() {
	s.mu.Lock()
	s.version++
	s.mu.Unlock()
}

// Dataset returns the cached dataset, reloading when a newer sync run has
// finished. Only runs that wrote data (succeeded or partial) count.
func (s *Store) Dataset(ctx context.Context) (*Dataset, error) {
	var runID int64
	err := s.Pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM sync_runs WHERE outcome IN ('succeeded', 'partial')`).Scan(&runID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.cached != nil && s.cached.RunID == runID && s.cachedV == s.version {
		d := s.cached
		s.mu.Unlock()
		return d, nil
	}
	v := s.version
	s.mu.Unlock()

	d, err := s.load(ctx, runID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cached, s.cachedV = d, v
	s.mu.Unlock()
	return d, nil
}

func (s *Store) load(ctx context.Context, runID int64) (*Dataset, error) {
	d := &Dataset{LoadedAt: time.Now(), RunID: runID, ProjectByID: map[int]*Project{}, IssuesByProject: map[int][]int{}, VersionByID: map[int]*risks.Version{}}
	err := pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT p.redmine_id, p.name, p.identifier, p.parent_redmine_id, p.status, p.last_synced_at,
				r.owner_name, r.tech_lead_name, r.verified_at
			FROM redmine_projects p LEFT JOIN project_roles r ON r.project_redmine_id = p.redmine_id
			WHERE p.in_scope AND p.inactive_since IS NULL ORDER BY p.name`)
		if err != nil {
			return err
		}
		d.Projects, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Project, error) {
			var p Project
			err := r.Scan(&p.ID, &p.Name, &p.Identifier, &p.ParentID, &p.Status, &p.LastSyncedAt, &p.Owner, &p.TechLead, &p.RolesVerifiedAt)
			return p, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT i.redmine_id, i.project_redmine_id, i.subject, i.tracker_name, i.status_name,
				i.priority_name, COALESCE(s.is_closed, i.closed_on IS NOT NULL), i.assigned_to_redmine_id, u.name,
				i.fixed_version_redmine_id, i.start_date, i.due_date, i.done_ratio, i.estimated_hours::float8,
				i.spent_hours::float8, i.created_on, i.updated_on, i.closed_on
			FROM redmine_issues i
			JOIN redmine_projects p ON p.redmine_id = i.project_redmine_id AND p.in_scope AND p.inactive_since IS NULL
			LEFT JOIN redmine_statuses s ON s.redmine_id = i.status_id
			LEFT JOIN redmine_users u ON u.redmine_id = i.assigned_to_redmine_id
			WHERE i.inactive_since IS NULL ORDER BY i.redmine_id`)
		if err != nil {
			return err
		}
		d.Issues, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (risks.Issue, error) {
			var is risks.Issue
			err := r.Scan(&is.ID, &is.ProjectID, &is.Subject, &is.TrackerName, &is.StatusName, &is.PriorityName, &is.IsClosed,
				&is.AssigneeID, &is.AssigneeName, &is.VersionID, &is.StartDate, &is.DueDate, &is.DoneRatio, &is.Estimated,
				&is.Spent, &is.CreatedOn, &is.UpdatedOn, &is.ClosedOn)
			return is, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT issue_from_redmine_id, issue_to_redmine_id, relation_type FROM redmine_issue_relations`)
		if err != nil {
			return err
		}
		d.Relations, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (risks.Relation, error) {
			var rel risks.Relation
			err := r.Scan(&rel.From, &rel.To, &rel.Type)
			return rel, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT redmine_id, project_redmine_id, name, status, effective_date FROM redmine_versions
			WHERE inactive_since IS NULL ORDER BY effective_date NULLS LAST, redmine_id`)
		if err != nil {
			return err
		}
		d.Versions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (risks.Version, error) {
			var v risks.Version
			err := r.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Status, &v.Due)
			return v, err
		})
		if err != nil {
			return err
		}
		return loadPlans(ctx, tx, d)
	})
	if err != nil {
		return nil, err
	}
	for i := range d.Projects {
		d.ProjectByID[d.Projects[i].ID] = &d.Projects[i]
	}
	for i, is := range d.Issues {
		d.IssuesByProject[is.ProjectID] = append(d.IssuesByProject[is.ProjectID], i)
	}
	for i := range d.Versions {
		d.VersionByID[d.Versions[i].ID] = &d.Versions[i]
	}
	return d, nil
}

// loadPlans adds planner milestones as versions with negative IDs and
// replaces Redmine values with the planner's where an issue has them.
func loadPlans(ctx context.Context, tx pgx.Tx, d *Dataset) error {
	rows, err := tx.Query(ctx, `SELECT -id, project_redmine_id, name, status, due_date FROM planner_milestones
		ORDER BY due_date NULLS LAST, id`)
	if err != nil {
		return err
	}
	ms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (risks.Version, error) {
		v := risks.Version{Planner: true}
		err := r.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Status, &v.Due)
		return v, err
	})
	if err != nil {
		return err
	}
	d.Versions = append(d.Versions, ms...)

	rows, err = tx.Query(ctx, `SELECT -t.id, t.project_redmine_id, t.subject, t.closed_on, t.created_at, t.updated_at
		FROM planner_tasks t JOIN redmine_projects p ON p.redmine_id = t.project_redmine_id AND p.in_scope AND p.inactive_since IS NULL
		ORDER BY t.id`)
	if err != nil {
		return err
	}
	tasks, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (risks.Issue, error) {
		is := risks.Issue{PlannerTask: true, TrackerName: "Planner task", PriorityName: "Normal"}
		err := r.Scan(&is.ID, &is.ProjectID, &is.Subject, &is.ClosedOn, &is.CreatedOn, &is.UpdatedOn)
		is.IsClosed = is.ClosedOn != nil
		is.StatusName = "Open"
		if is.IsClosed {
			is.StatusName = "Done"
		}
		return is, err
	})
	if err != nil {
		return err
	}
	d.Issues = append(d.Issues, tasks...)

	rows, err = tx.Query(ctx, `SELECT p.issue_redmine_id, p.estimated_hours::float8, p.due_date, p.assignee_redmine_id, u.name, p.version_id
		FROM issue_plans p LEFT JOIN redmine_users u ON u.redmine_id = p.assignee_redmine_id`)
	if err != nil {
		return err
	}
	type plan struct {
		est      *float64
		due      *time.Time
		assignee *int
		name     *string
		version  *int
	}
	plans := map[int]plan{}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (int, error) {
		var id int
		var p plan
		err := r.Scan(&id, &p.est, &p.due, &p.assignee, &p.name, &p.version)
		plans[id] = p
		return id, err
	})
	if err != nil {
		return err
	}
	for i := range d.Issues {
		pl, ok := plans[d.Issues[i].ID]
		if !ok {
			continue
		}
		is := &d.Issues[i]
		set := func(field string, redmine any) {
			if is.RedmineValues == nil {
				is.RedmineValues = map[string]any{}
			}
			is.Planned = append(is.Planned, field)
			is.RedmineValues[field] = redmine
		}
		if pl.est != nil {
			set("estimated_hours", is.Estimated)
			is.Estimated = pl.est
		}
		if pl.due != nil {
			set("due_date", is.DueDate)
			is.DueDate = pl.due
		}
		if pl.assignee != nil {
			set("assignee_id", is.AssigneeID)
			is.AssigneeID, is.AssigneeName = pl.assignee, pl.name
		}
		if pl.version != nil {
			set("version_id", is.VersionID)
			is.VersionID = pl.version
		}
	}
	return nil
}

// Subtree returns the project and its in-scope descendants, the project
// first and the rest by ID.
func (d *Dataset) Subtree(id int) []int {
	children := map[int][]int{}
	for _, p := range d.Projects {
		if p.ParentID != nil {
			children[*p.ParentID] = append(children[*p.ParentID], p.ID)
		}
	}
	out := []int{id}
	seen := map[int]bool{id: true}
	stack := append([]int(nil), children[id]...)
	var rest []int
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[c] {
			continue
		}
		seen[c] = true
		rest = append(rest, c)
		stack = append(stack, children[c]...)
	}
	sort.Ints(rest)
	return append(out, rest...)
}

// ProjectIssues returns the issues of one project.
func (d *Dataset) ProjectIssues(id int) []risks.Issue {
	idx := d.IssuesByProject[id]
	out := make([]risks.Issue, len(idx))
	for i, j := range idx {
		out[i] = d.Issues[j]
	}
	return out
}

// SyncRun is one row of sync_runs.
type SyncRun struct {
	ID               int64           `json:"id"`
	Kind             string          `json:"kind"`
	StartedAt        time.Time       `json:"started_at"`
	HeartbeatAt      time.Time       `json:"heartbeat_at"`
	FinishedAt       *time.Time      `json:"finished_at"`
	Outcome          string          `json:"outcome"`
	Counts           json.RawMessage `json:"counts"`
	CheckpointBefore *time.Time      `json:"checkpoint_before"`
	CheckpointAfter  *time.Time      `json:"checkpoint_after"`
	Warnings         []string        `json:"warnings"`
	ErrorSummary     *string         `json:"error_summary"`
}

// Reconciliation is one per-project total comparison.
type Reconciliation struct {
	ProjectID    int     `json:"project_id"`
	APITotal     *int    `json:"api_total"`
	StoredActive int     `json:"stored_active"`
	Matches      bool    `json:"matches"`
	Note         *string `json:"note"`
}

// SyncStatus is the data behind the sync status screen.
type SyncStatus struct {
	Runs                 []SyncRun        `json:"runs"`
	LastSuccess          *SyncRun         `json:"last_success"`
	Checkpoint           *time.Time       `json:"checkpoint"`
	LatestReconciliation []Reconciliation `json:"latest_reconciliation"`
}

const runCols = `id, kind, started_at, heartbeat_at, finished_at, outcome, counts, checkpoint_before, checkpoint_after, warnings, error_summary`

func scanRun(r pgx.Row) (SyncRun, error) {
	var s SyncRun
	var warnings []byte
	err := r.Scan(&s.ID, &s.Kind, &s.StartedAt, &s.HeartbeatAt, &s.FinishedAt, &s.Outcome, &s.Counts, &s.CheckpointBefore,
		&s.CheckpointAfter, &warnings, &s.ErrorSummary)
	if err == nil {
		json.Unmarshal(warnings, &s.Warnings)
		if s.Warnings == nil {
			s.Warnings = []string{}
		}
	}
	return s, err
}

// LoadSyncStatus returns recent runs and reconciliation.
func (s *Store) LoadSyncStatus(ctx context.Context, limit int) (*SyncStatus, error) {
	st := &SyncStatus{}
	rows, err := s.Pool.Query(ctx, `SELECT `+runCols+` FROM sync_runs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	st.Runs, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SyncRun, error) { return scanRun(r) })
	if err != nil {
		return nil, err
	}
	last, err := scanRun(s.Pool.QueryRow(ctx, `SELECT `+runCols+` FROM sync_runs WHERE outcome = 'succeeded' ORDER BY id DESC LIMIT 1`))
	if err == nil {
		st.LastSuccess = &last
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	err = s.Pool.QueryRow(ctx, `SELECT value FROM sync_checkpoints WHERE name = 'issues_updated_on'`).Scan(&st.Checkpoint)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT project_redmine_id, api_total, stored_active, matches, note FROM sync_reconciliation
		WHERE run_id = (SELECT max(run_id) FROM sync_reconciliation) ORDER BY matches, project_redmine_id`)
	if err != nil {
		return nil, err
	}
	st.LatestReconciliation, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Reconciliation, error) {
		var x Reconciliation
		err := r.Scan(&x.ProjectID, &x.APITotal, &x.StoredActive, &x.Matches, &x.Note)
		return x, err
	})
	return st, err
}

// LastSuccessfulSync returns when the newest succeeded run finished.
func (s *Store) LastSuccessfulSync(ctx context.Context) (*time.Time, error) {
	var t *time.Time
	err := s.Pool.QueryRow(ctx, `SELECT max(finished_at) FROM sync_runs WHERE outcome = 'succeeded'`).Scan(&t)
	return t, err
}

// Rules returns the rule settings stored by admins, merged over defaults.
func (s *Store) Rules(ctx context.Context, today time.Time) (risks.Rules, error) {
	r := risks.DefaultRules(today)
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM planner_settings WHERE key = 'rules'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	r.Today = today
	return r, nil
}

// SaveRules stores rule settings and audits the change.
func (s *Store) SaveRules(ctx context.Context, r risks.Rules, userID int64, actor string) error {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var before []byte
		tx.QueryRow(ctx, `SELECT value FROM planner_settings WHERE key = 'rules'`).Scan(&before)
		if _, err := tx.Exec(ctx, `INSERT INTO planner_settings (key, value, updated_by, updated_at) VALUES ('rules', $1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`, r, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, actor, "update", "planner_settings", "rules", rawOrNil(before), r)
	})
	s.Invalidate()
	return err
}

// SetProjectRoles records the owner and tech lead for a project and
// audits the change.
func (s *Store) SetProjectRoles(ctx context.Context, projectID int, owner, techLead *string, userID int64, actor string) error {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var before map[string]any
		var o, t *string
		if err := tx.QueryRow(ctx, `SELECT owner_name, tech_lead_name FROM project_roles WHERE project_redmine_id = $1`, projectID).Scan(&o, &t); err == nil {
			before = map[string]any{"owner": o, "tech_lead": t}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO project_roles (project_redmine_id, owner_name, tech_lead_name, verified_by, verified_at, updated_at)
			VALUES ($1, $2, $3, $4, now(), now()) ON CONFLICT (project_redmine_id) DO UPDATE SET owner_name = EXCLUDED.owner_name,
			tech_lead_name = EXCLUDED.tech_lead_name, verified_by = EXCLUDED.verified_by, verified_at = now(), updated_at = now()`,
			projectID, owner, techLead, userID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, actor, "update", "project_roles", strconv.Itoa(projectID), before,
			map[string]any{"owner": owner, "tech_lead": techLead})
	})
	s.Invalidate()
	return err
}

func rawOrNil(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return json.RawMessage(b)
}
