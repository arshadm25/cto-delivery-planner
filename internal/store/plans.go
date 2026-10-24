package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/arshadm25/cto-delivery-planner/internal/audit"
)

// ErrPlanNotFound means the planner milestone does not exist.
var ErrPlanNotFound = errors.New("store: not found")

// PlanField is one field of a plan change. Set false leaves the field as it
// is; Set true with a nil Value goes back to the Redmine value.
type PlanField[T any] struct {
	Set   bool
	Value *T
}

// PlanChange is what to change on a set of issues. Nothing is written to
// Redmine.
type PlanChange struct {
	Estimated PlanField[float64]
	DueDate   PlanField[time.Time]
	Assignee  PlanField[int]
	VersionID PlanField[int]
}

// SetIssuePlans applies one change to every listed issue and audits it.
func (s *Store) SetIssuePlans(ctx context.Context, issueIDs []int, c PlanChange, userID int64, actor string) error {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, id := range issueIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO issue_plans (issue_redmine_id, estimated_hours, due_date, assignee_redmine_id, version_id, updated_by)
				VALUES ($1, $3, $5, $7, $9, $10)
				ON CONFLICT (issue_redmine_id) DO UPDATE SET
					estimated_hours = CASE WHEN $2 THEN EXCLUDED.estimated_hours ELSE issue_plans.estimated_hours END,
					due_date = CASE WHEN $4 THEN EXCLUDED.due_date ELSE issue_plans.due_date END,
					assignee_redmine_id = CASE WHEN $6 THEN EXCLUDED.assignee_redmine_id ELSE issue_plans.assignee_redmine_id END,
					version_id = CASE WHEN $8 THEN EXCLUDED.version_id ELSE issue_plans.version_id END,
					updated_by = EXCLUDED.updated_by, updated_at = now()`,
				id, c.Estimated.Set, c.Estimated.Value, c.DueDate.Set, c.DueDate.Value, c.Assignee.Set, c.Assignee.Value,
				c.VersionID.Set, c.VersionID.Value, userID); err != nil {
				return err
			}
		}
		if err := dropEmptyPlans(ctx, tx); err != nil {
			return err
		}
		return audit.Record(ctx, tx, actor, "plan", "issue_plans", "", nil, map[string]any{"issues": issueIDs, "change": planAudit(c)})
	})
	s.Invalidate()
	return err
}

func dropEmptyPlans(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `DELETE FROM issue_plans WHERE estimated_hours IS NULL AND due_date IS NULL
		AND assignee_redmine_id IS NULL AND version_id IS NULL`)
	return err
}

func planAudit(c PlanChange) map[string]any {
	out := map[string]any{}
	if c.Estimated.Set {
		out["estimated_hours"] = c.Estimated.Value
	}
	if c.DueDate.Set {
		out["due_date"] = c.DueDate.Value
	}
	if c.Assignee.Set {
		out["assignee_id"] = c.Assignee.Value
	}
	if c.VersionID.Set {
		out["version_id"] = c.VersionID.Value
	}
	return out
}

// MilestoneInput creates or changes a planner milestone.
type MilestoneInput struct {
	Name    string
	DueDate *time.Time
	Status  string
}

// SaveMilestone creates (id 0) or updates a planner milestone of a project
// and returns its version ID, which is negative.
func (s *Store) SaveMilestone(ctx context.Context, projectID int, id int64, in MilestoneInput, userID int64, actor string) (int, error) {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		action := "update"
		if id == 0 {
			action = "create"
			if err := tx.QueryRow(ctx, `INSERT INTO planner_milestones (project_redmine_id, name, due_date, status, created_by)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`, projectID, in.Name, in.DueDate, in.Status, userID).Scan(&id); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE planner_milestones SET name = $3, due_date = $4, status = $5, updated_at = now()
				WHERE id = $1 AND project_redmine_id = $2`, id, projectID, in.Name, in.DueDate, in.Status)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrPlanNotFound
			}
		}
		return audit.Record(ctx, tx, actor, action, "planner_milestones", strconv.FormatInt(id, 10), nil,
			map[string]any{"project_id": projectID, "name": in.Name, "due_date": in.DueDate, "status": in.Status})
	})
	s.Invalidate()
	return int(-id), err
}

// DeleteMilestone removes a planner milestone. Issues planned into it go
// back to their Redmine version.
func (s *Store) DeleteMilestone(ctx context.Context, projectID int, id int64, actor string) error {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM delivery_commitments WHERE version_redmine_id = $1
			AND status IN ('proposed', 'approved', 'delivered'))`, -id).Scan(&live); err != nil {
			return err
		}
		if live {
			return ErrMilestoneCommitted
		}
		tag, err := tx.Exec(ctx, `DELETE FROM planner_milestones WHERE id = $1 AND project_redmine_id = $2`, id, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrPlanNotFound
		}
		if _, err := tx.Exec(ctx, `UPDATE issue_plans SET version_id = NULL WHERE version_id = $1`, -id); err != nil {
			return err
		}
		if err := dropEmptyPlans(ctx, tx); err != nil {
			return err
		}
		return audit.Record(ctx, tx, actor, "delete", "planner_milestones", strconv.FormatInt(id, 10), nil, nil)
	})
	s.Invalidate()
	return err
}

// ErrMilestoneCommitted is returned when deleting a milestone that a
// commitment points at.
var ErrMilestoneCommitted = errors.New("this milestone has a commitment; reject or supersede it first")
