// Package planning stores planner-owned records: delivery commitments,
// capacity allocations, the working calendar and project snapshots. Every
// change to a commitment or allocation is written to audit_events in the
// same transaction.
package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arshadm25/cto-delivery-planner/internal/audit"
	"github.com/arshadm25/cto-delivery-planner/internal/forecast"
)

// ErrInvalid is returned for requests that break a rule; its message is
// safe to show to the user.
type ErrInvalid struct{ Msg string }

func (e *ErrInvalid) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ErrInvalid{Msg: fmt.Sprintf(format, a...)} }

// ErrNotFound means the record does not exist.
var ErrNotFound = errors.New("planning: not found")

// Store wraps the pool.
type Store struct{ Pool *pgxpool.Pool }

// Actor identifies who is acting.
type Actor struct {
	ID    int64
	Email string
}

func (a Actor) String() string { return audit.Actor(a.ID, a.Email) }

// Commitment is a delivery commitment row.
type Commitment struct {
	ID           int64      `json:"id"`
	ProjectID    int        `json:"project_id"`
	VersionID    *int       `json:"version_id"`
	Title        string     `json:"title"`
	BaselineDate time.Time  `json:"baseline_date"`
	Status       string     `json:"status"`
	ActualDate   *time.Time `json:"actual_date"`
	Reason       string     `json:"reason"`
	ProposedBy   string     `json:"proposed_by"`
	ProposedAt   time.Time  `json:"proposed_at"`
	DecidedBy    *string    `json:"approved_by"`
	DecidedAt    *time.Time `json:"approved_at"`
	DecisionNote *string    `json:"decision_note"`
	SupersedesID *int64     `json:"supersedes_id"`
}

const commitmentCols = `c.id, c.project_redmine_id, c.version_redmine_id, c.title, c.baseline_date, c.status, c.actual_date,
	c.reason, pu.display_name, c.proposed_at, du.display_name, c.decided_at, c.decision_note, c.supersedes_id`

const commitmentFrom = ` FROM delivery_commitments c JOIN app_users pu ON pu.id = c.proposed_by
	LEFT JOIN app_users du ON du.id = c.decided_by`

func scanCommitment(r pgx.Row) (Commitment, error) {
	var c Commitment
	err := r.Scan(&c.ID, &c.ProjectID, &c.VersionID, &c.Title, &c.BaselineDate, &c.Status, &c.ActualDate, &c.Reason,
		&c.ProposedBy, &c.ProposedAt, &c.DecidedBy, &c.DecidedAt, &c.DecisionNote, &c.SupersedesID)
	return c, err
}

// Commitments returns every commitment for the given projects, newest first.
func (s *Store) Commitments(ctx context.Context, projectIDs []int) ([]Commitment, error) {
	ids := make([]int32, len(projectIDs))
	for i, id := range projectIDs {
		ids[i] = int32(id)
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+commitmentCols+commitmentFrom+` WHERE c.project_redmine_id = ANY($1)
		ORDER BY c.proposed_at DESC, c.id DESC`, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Commitment, error) { return scanCommitment(r) })
}

// Commitment returns one commitment.
func (s *Store) Commitment(ctx context.Context, id int64) (Commitment, error) {
	c, err := scanCommitment(s.Pool.QueryRow(ctx, `SELECT `+commitmentCols+commitmentFrom+` WHERE c.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// ProposeInput is a new baseline or a revision.
type ProposeInput struct {
	ProjectID    int    `json:"project_id"`
	VersionID    *int   `json:"version_id"`
	Title        string `json:"title"`
	BaselineDate string `json:"baseline_date"`
	Reason       string `json:"reason"`
}

// Propose records a proposed commitment. If an approved one exists for the
// same project or version, the proposal is a revision of it.
func (s *Store) Propose(ctx context.Context, by Actor, in ProposeInput) (Commitment, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Title == "" || in.Reason == "" {
		return Commitment{}, invalid("title and reason are required")
	}
	date, err := time.Parse(time.DateOnly, in.BaselineDate)
	if err != nil {
		return Commitment{}, invalid("baseline_date must be YYYY-MM-DD")
	}
	var id int64
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var pending int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM delivery_commitments WHERE project_redmine_id = $1
			AND COALESCE(version_redmine_id, 0) = COALESCE($2, 0) AND status = 'proposed'`, in.ProjectID, in.VersionID).Scan(&pending); err != nil {
			return err
		}
		if pending > 0 {
			return invalid("a proposal for this scope is already waiting for a decision")
		}
		var current *int64
		err := tx.QueryRow(ctx, `SELECT id FROM delivery_commitments WHERE project_redmine_id = $1
			AND COALESCE(version_redmine_id, 0) = COALESCE($2, 0) AND status = 'approved'`, in.ProjectID, in.VersionID).Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO delivery_commitments (project_redmine_id, version_redmine_id, title, baseline_date,
				status, reason, proposed_by, supersedes_id) VALUES ($1, $2, $3, $4, 'proposed', $5, $6, $7) RETURNING id`,
			in.ProjectID, in.VersionID, in.Title, date, in.Reason, by.ID, current).Scan(&id); err != nil {
			return err
		}
		return audit.Record(ctx, tx, by.String(), "propose", "delivery_commitments", strconv.FormatInt(id, 10), nil, in)
	})
	if err != nil {
		return Commitment{}, err
	}
	return s.Commitment(ctx, id)
}

// ScopeItem is one issue in a commitment's scope, frozen on approval.
type ScopeItem struct {
	IssueID   int      `json:"issue_id"`
	Estimated *float64 `json:"estimated_hours"`
	Open      bool     `json:"open"`
}

// Scopes returns the frozen scope of each listed commitment that has one.
func (s *Store) Scopes(ctx context.Context, ids []int64) (map[int64][]ScopeItem, error) {
	out := map[int64][]ScopeItem{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT commitment_id, issue_redmine_id, estimated_hours::float8, was_open
		FROM commitment_scope WHERE commitment_id = ANY($1) ORDER BY commitment_id, issue_redmine_id`, ids)
	if err != nil {
		return nil, err
	}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (int64, error) {
		var id int64
		var it ScopeItem
		if err := r.Scan(&id, &it.IssueID, &it.Estimated, &it.Open); err != nil {
			return 0, err
		}
		out[id] = append(out[id], it)
		return id, nil
	})
	return out, err
}

// Decide approves or rejects a proposal. Approving a revision supersedes
// the previous baseline; its row and date are kept for history. On
// approval, scope is the work in the commitment's scope at that moment; it
// is frozen so later scope changes can be shown against the baseline.
func (s *Store) Decide(ctx context.Context, by Actor, id int64, approve bool, note string, scope []ScopeItem) (Commitment, error) {
	note = strings.TrimSpace(note)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var status string
		var supersedes *int64
		err := tx.QueryRow(ctx, `SELECT status, supersedes_id FROM delivery_commitments WHERE id = $1 FOR UPDATE`, id).Scan(&status, &supersedes)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if status != "proposed" {
			return invalid("only a proposed commitment can be decided; this one is %s", status)
		}
		next := "rejected"
		if approve {
			next = "approved"
			if supersedes != nil {
				if _, err := tx.Exec(ctx, `UPDATE delivery_commitments SET status = 'superseded' WHERE id = $1 AND status = 'approved'`, *supersedes); err != nil {
					return err
				}
			}
		} else if note == "" {
			return invalid("a note is required when rejecting")
		}
		if _, err := tx.Exec(ctx, `UPDATE delivery_commitments SET status = $2, decided_by = $3, decided_at = now(),
			decision_note = NULLIF($4, '') WHERE id = $1`, id, next, by.ID, note); err != nil {
			return err
		}
		if approve {
			for _, it := range scope {
				if _, err := tx.Exec(ctx, `INSERT INTO commitment_scope (commitment_id, issue_redmine_id, estimated_hours, was_open)
					VALUES ($1, $2, $3, $4)`, id, it.IssueID, it.Estimated, it.Open); err != nil {
					return err
				}
			}
		}
		return audit.Record(ctx, tx, by.String(), next, "delivery_commitments", strconv.FormatInt(id, 10),
			map[string]any{"status": status}, map[string]any{"status": next, "note": note})
	})
	if err != nil {
		return Commitment{}, err
	}
	return s.Commitment(ctx, id)
}

// MarkDelivered records the actual delivery date of an approved commitment.
func (s *Store) MarkDelivered(ctx context.Context, by Actor, id int64, actual string) (Commitment, error) {
	date, err := time.Parse(time.DateOnly, actual)
	if err != nil {
		return Commitment{}, invalid("actual_date must be YYYY-MM-DD")
	}
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE delivery_commitments SET status = 'delivered', actual_date = $2 WHERE id = $1 AND status = 'approved'`, id, date)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return invalid("only an approved commitment can be marked delivered")
		}
		return audit.Record(ctx, tx, by.String(), "deliver", "delivery_commitments", strconv.FormatInt(id, 10), nil, map[string]any{"actual_date": actual})
	})
	if err != nil {
		return Commitment{}, err
	}
	return s.Commitment(ctx, id)
}

// Allocation is a capacity allocation for one person or team and period.
type Allocation struct {
	ID             int64           `json:"id"`
	PersonID       *int            `json:"person_id"`
	PersonName     *string         `json:"person_name"`
	TeamID         *int64          `json:"team_id"`
	TeamName       *string         `json:"team_name"`
	PeriodStart    time.Time       `json:"period_start"`
	PeriodEnd      time.Time       `json:"period_end"`
	AvailableHours *float64        `json:"available_hours"`
	Assumptions    string          `json:"assumptions"`
	Status         string          `json:"status"`
	CreatedBy      string          `json:"created_by"`
	ApprovedBy     *string         `json:"approved_by"`
	ApprovedAt     *time.Time      `json:"approved_at"`
	Projects       map[int]float64 `json:"projects"`
}

// AllocationInput creates or replaces an allocation.
type AllocationInput struct {
	PersonID       *int            `json:"person_id"`
	TeamID         *int64          `json:"team_id"`
	TeamName       *string         `json:"team_name"`
	PeriodStart    string          `json:"period_start"`
	PeriodEnd      string          `json:"period_end"`
	AvailableHours *float64        `json:"available_hours"`
	Assumptions    string          `json:"assumptions"`
	Projects       map[int]float64 `json:"projects"`
}

func (in *AllocationInput) validate() (time.Time, time.Time, error) {
	if in.TeamName != nil && strings.TrimSpace(*in.TeamName) == "" {
		in.TeamName = nil
	}
	set := 0
	for _, ok := range []bool{in.PersonID != nil, in.TeamID != nil, in.TeamName != nil} {
		if ok {
			set++
		}
	}
	if set != 1 {
		return time.Time{}, time.Time{}, invalid("set exactly one of person_id, team_id or team_name")
	}
	start, err1 := time.Parse(time.DateOnly, in.PeriodStart)
	end, err2 := time.Parse(time.DateOnly, in.PeriodEnd)
	if err1 != nil || err2 != nil || end.Before(start) {
		return start, end, invalid("period_start and period_end must be YYYY-MM-DD with end on or after start")
	}
	if in.AvailableHours != nil && *in.AvailableHours < 0 {
		return start, end, invalid("available_hours cannot be negative")
	}
	var total float64
	for _, h := range in.Projects {
		if h < 0 {
			return start, end, invalid("allocated hours cannot be negative")
		}
		total += h
	}
	if in.AvailableHours != nil && total > *in.AvailableHours {
		return start, end, invalid("allocated hours (%.1f) exceed available hours (%.1f)", total, *in.AvailableHours)
	}
	return start, end, nil
}

// SaveAllocation creates (id 0) or replaces an allocation. Any change puts
// it back to draft, so an approved allocation is never silently edited.
func (s *Store) SaveAllocation(ctx context.Context, by Actor, id int64, in AllocationInput) (int64, error) {
	start, end, err := in.validate()
	if err != nil {
		return 0, err
	}
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if in.TeamID != nil {
			var known bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM planner_teams WHERE id = $1)`, *in.TeamID).Scan(&known); err != nil {
				return err
			}
			if !known {
				return invalid("unknown team")
			}
		}
		action := "update"
		if id == 0 {
			action = "create"
			if err := tx.QueryRow(ctx, `INSERT INTO capacity_allocations (person_redmine_id, team_name, period_start, period_end,
					available_hours, assumptions, status, created_by, team_id) VALUES ($1, $2, $3, $4, $5, $6, 'draft', $7, $8) RETURNING id`,
				in.PersonID, in.TeamName, start, end, in.AvailableHours, in.Assumptions, by.ID, in.TeamID).Scan(&id); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE capacity_allocations SET person_redmine_id = $2, team_name = $3, period_start = $4,
				period_end = $5, available_hours = $6, assumptions = $7, team_id = $8, status = 'draft', approved_by = NULL, approved_at = NULL
				WHERE id = $1`, id, in.PersonID, in.TeamName, start, end, in.AvailableHours, in.Assumptions, in.TeamID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
			if _, err := tx.Exec(ctx, `DELETE FROM capacity_allocation_projects WHERE allocation_id = $1`, id); err != nil {
				return err
			}
		}
		for pid, h := range in.Projects {
			if _, err := tx.Exec(ctx, `INSERT INTO capacity_allocation_projects (allocation_id, project_redmine_id, allocated_hours)
				VALUES ($1, $2, $3)`, id, pid, h); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, by.String(), action, "capacity_allocations", strconv.FormatInt(id, 10), nil, in)
	})
	return id, err
}

// ApproveAllocation marks a draft approved.
func (s *Store) ApproveAllocation(ctx context.Context, by Actor, id int64) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE capacity_allocations SET status = 'approved', approved_by = $2, approved_at = now()
			WHERE id = $1 AND status = 'draft'`, id, by.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return invalid("only a draft allocation can be approved")
		}
		return audit.Record(ctx, tx, by.String(), "approve", "capacity_allocations", strconv.FormatInt(id, 10), nil, nil)
	})
}

// DeleteAllocation removes a draft.
func (s *Store) DeleteAllocation(ctx context.Context, by Actor, id int64) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM capacity_allocations WHERE id = $1 AND status = 'draft'`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return invalid("only a draft allocation can be deleted")
		}
		return audit.Record(ctx, tx, by.String(), "delete", "capacity_allocations", strconv.FormatInt(id, 10), nil, nil)
	})
}

// Allocations returns allocations overlapping [from, to].
func (s *Store) Allocations(ctx context.Context, from, to time.Time) ([]Allocation, error) {
	rows, err := s.Pool.Query(ctx, `SELECT a.id, a.person_redmine_id, u.name, a.team_id, COALESCE(t.name, a.team_name), a.period_start, a.period_end,
			a.available_hours::float8, a.assumptions, a.status, cu.display_name, au.display_name, a.approved_at
		FROM capacity_allocations a JOIN app_users cu ON cu.id = a.created_by LEFT JOIN app_users au ON au.id = a.approved_by
		LEFT JOIN redmine_users u ON u.redmine_id = a.person_redmine_id LEFT JOIN planner_teams t ON t.id = a.team_id
		WHERE a.period_end >= $1 AND a.period_start <= $2 ORDER BY a.period_start, a.id`, from, to)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Allocation, error) {
		var a Allocation
		err := r.Scan(&a.ID, &a.PersonID, &a.PersonName, &a.TeamID, &a.TeamName, &a.PeriodStart, &a.PeriodEnd, &a.AvailableHours,
			&a.Assumptions, &a.Status, &a.CreatedBy, &a.ApprovedBy, &a.ApprovedAt)
		a.Projects = map[int]float64{}
		return a, err
	})
	if err != nil || len(out) == 0 {
		return out, err
	}
	idx := map[int64]int{}
	ids := make([]int64, len(out))
	for i, a := range out {
		idx[a.ID] = i
		ids[i] = a.ID
	}
	rows, err = s.Pool.Query(ctx, `SELECT allocation_id, project_redmine_id, allocated_hours::float8 FROM capacity_allocation_projects
		WHERE allocation_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var aid int64
		var pid int
		var h float64
		if err := rows.Scan(&aid, &pid, &h); err != nil {
			return nil, err
		}
		out[idx[aid]].Projects[pid] = h
	}
	return out, rows.Err()
}

// Calendar returns the stored working calendar or the unconfirmed default.
func (s *Store) Calendar(ctx context.Context) (forecast.Calendar, error) {
	c := forecast.DefaultCalendar()
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM planner_settings WHERE key = 'calendar'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(raw, &c)
	return c, err
}

// SaveCalendar stores the calendar and marks it confirmed.
func (s *Store) SaveCalendar(ctx context.Context, by Actor, c forecast.Calendar) error {
	if c.HoursPerDay <= 0 || c.HoursPerDay > 24 || len(c.WorkingDays) == 0 {
		return invalid("hours_per_day must be 1 to 24 and at least one working day is required")
	}
	for _, h := range c.Holidays {
		if _, err := time.Parse(time.DateOnly, h); err != nil {
			return invalid("holiday %q must be YYYY-MM-DD", h)
		}
	}
	c.Confirmed = true
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO planner_settings (key, value, updated_by, updated_at) VALUES ('calendar', $1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`, c, by.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, by.String(), "update", "planner_settings", "calendar", nil, c)
	})
}

// LoggedHours returns hours logged per person between from and to
// (inclusive) on the given projects.
func (s *Store) LoggedHours(ctx context.Context, projectIDs []int, from, to time.Time) (map[int]float64, error) {
	ids := make([]int32, len(projectIDs))
	for i, id := range projectIDs {
		ids[i] = int32(id)
	}
	rows, err := s.Pool.Query(ctx, `SELECT user_redmine_id, sum(hours)::float8 FROM redmine_time_entries
		WHERE inactive_since IS NULL AND project_redmine_id = ANY($1) AND spent_on BETWEEN $2 AND $3 GROUP BY 1`, ids, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]float64{}
	for rows.Next() {
		var id int
		var h float64
		if err := rows.Scan(&id, &h); err != nil {
			return nil, err
		}
		out[id] = h
	}
	return out, rows.Err()
}

// TimeEntriesPresent reports whether any time entries were imported.
func (s *Store) TimeEntriesPresent(ctx context.Context) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM redmine_time_entries)`).Scan(&ok)
	return ok, err
}

// Snapshot is one project's stored daily state.
type Snapshot struct {
	Date                    time.Time      `json:"date"`
	ProjectID               int            `json:"project_id"`
	TakenAt                 time.Time      `json:"taken_at"`
	Health                  string         `json:"health"`
	HealthReasons           []string       `json:"health_reasons"`
	Baseline                *time.Time     `json:"baseline"`
	Forecast                *time.Time     `json:"forecast"`
	Confidence              string         `json:"confidence"`
	Method                  string         `json:"method"`
	Open                    int            `json:"open"`
	Closed                  int            `json:"closed"`
	Overdue                 int            `json:"overdue"`
	Blocked                 int            `json:"blocked"`
	RemainingEstimatedHours *float64       `json:"remaining_estimated_hours"`
	EstimateCoverage        *float64       `json:"estimate_coverage"`
	FlagCounts              map[string]int `json:"flag_counts"`
}

// SaveSnapshots upserts one day's snapshots and prunes rows older than
// retentionDays. It returns how many rows were pruned.
func (s *Store) SaveSnapshots(ctx context.Context, snaps []Snapshot, retentionDays int) (int64, error) {
	var pruned int64
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, sn := range snaps {
			if _, err := tx.Exec(ctx, `INSERT INTO project_snapshots (snapshot_date, project_redmine_id, taken_at, health, health_reasons,
					baseline_date, forecast_date, forecast_confidence, forecast_method, open_issues, closed_issues, overdue_issues,
					blocked_issues, remaining_estimated_hours, estimate_coverage, flag_counts)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
				ON CONFLICT (snapshot_date, project_redmine_id) DO UPDATE SET taken_at = EXCLUDED.taken_at, health = EXCLUDED.health,
					health_reasons = EXCLUDED.health_reasons, baseline_date = EXCLUDED.baseline_date, forecast_date = EXCLUDED.forecast_date,
					forecast_confidence = EXCLUDED.forecast_confidence, forecast_method = EXCLUDED.forecast_method,
					open_issues = EXCLUDED.open_issues, closed_issues = EXCLUDED.closed_issues, overdue_issues = EXCLUDED.overdue_issues,
					blocked_issues = EXCLUDED.blocked_issues, remaining_estimated_hours = EXCLUDED.remaining_estimated_hours,
					estimate_coverage = EXCLUDED.estimate_coverage, flag_counts = EXCLUDED.flag_counts`,
				sn.Date, sn.ProjectID, sn.TakenAt, sn.Health, sn.HealthReasons, sn.Baseline, sn.Forecast, sn.Confidence, sn.Method,
				sn.Open, sn.Closed, sn.Overdue, sn.Blocked, sn.RemainingEstimatedHours, sn.EstimateCoverage, sn.FlagCounts); err != nil {
				return err
			}
		}
		if retentionDays > 0 && len(snaps) > 0 {
			tag, err := tx.Exec(ctx, `DELETE FROM project_snapshots WHERE snapshot_date < $1`, snaps[0].Date.AddDate(0, 0, -retentionDays))
			if err != nil {
				return err
			}
			pruned = tag.RowsAffected()
		}
		return nil
	})
	return pruned, err
}

// SnapshotsOn returns snapshots for the latest date on or before day.
func (s *Store) SnapshotsOn(ctx context.Context, day time.Time) ([]Snapshot, error) {
	rows, err := s.Pool.Query(ctx, `SELECT snapshot_date, project_redmine_id, taken_at, health, health_reasons, baseline_date,
			forecast_date, forecast_confidence, forecast_method, open_issues, closed_issues, overdue_issues, blocked_issues,
			remaining_estimated_hours::float8, estimate_coverage::float8, flag_counts
		FROM project_snapshots WHERE snapshot_date = (SELECT max(snapshot_date) FROM project_snapshots WHERE snapshot_date <= $1)`, day)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Snapshot, error) {
		var sn Snapshot
		err := r.Scan(&sn.Date, &sn.ProjectID, &sn.TakenAt, &sn.Health, &sn.HealthReasons, &sn.Baseline, &sn.Forecast,
			&sn.Confidence, &sn.Method, &sn.Open, &sn.Closed, &sn.Overdue, &sn.Blocked, &sn.RemainingEstimatedHours,
			&sn.EstimateCoverage, &sn.FlagCounts)
		return sn, err
	})
}

// History returns a project's snapshots, oldest first.
func (s *Store) History(ctx context.Context, projectID int, since time.Time) ([]Snapshot, error) {
	rows, err := s.Pool.Query(ctx, `SELECT snapshot_date, project_redmine_id, taken_at, health, health_reasons, baseline_date,
			forecast_date, forecast_confidence, forecast_method, open_issues, closed_issues, overdue_issues, blocked_issues,
			remaining_estimated_hours::float8, estimate_coverage::float8, flag_counts
		FROM project_snapshots WHERE project_redmine_id = $1 AND snapshot_date >= $2 ORDER BY snapshot_date`, projectID, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Snapshot, error) {
		var sn Snapshot
		err := r.Scan(&sn.Date, &sn.ProjectID, &sn.TakenAt, &sn.Health, &sn.HealthReasons, &sn.Baseline, &sn.Forecast,
			&sn.Confidence, &sn.Method, &sn.Open, &sn.Closed, &sn.Overdue, &sn.Blocked, &sn.RemainingEstimatedHours,
			&sn.EstimateCoverage, &sn.FlagCounts)
		return sn, err
	})
}
