package syncer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
)

// issueRow is an issue validated and converted for the database.
type issueRow struct {
	id, projectID                             int
	parentID, authorID, assigneeID, versionID *int
	subject                                   string
	trackerID, statusID, priorityID           int
	trackerName, statusName, priorityName     string
	startDate, dueDate                        *time.Time
	doneRatio                                 *int
	estimated, spent                          *float64
	isPrivate                                 *bool
	createdOn, updatedOn                      time.Time
	closedOn                                  *time.Time
	hash                                      string
}

func toIssueRow(is redmine.Issue) (issueRow, error) {
	row := issueRow{
		id: is.ID, projectID: is.Project.ID, subject: is.Subject,
		trackerID: is.Tracker.ID, trackerName: is.Tracker.Name,
		statusID: is.Status.ID, statusName: is.Status.Name,
		priorityID: is.Priority.ID, priorityName: is.Priority.Name,
		doneRatio: is.DoneRatio, estimated: is.EstimatedHours, spent: is.SpentHours, isPrivate: is.IsPrivate,
	}
	if is.ID <= 0 || is.Project.ID <= 0 {
		return row, fmt.Errorf("missing issue or project ID")
	}
	if is.Parent != nil {
		row.parentID = &is.Parent.ID
	}
	if is.Author != nil {
		row.authorID = &is.Author.ID
	}
	if is.AssignedTo != nil {
		row.assigneeID = &is.AssignedTo.ID
	}
	if is.FixedVersion != nil {
		row.versionID = &is.FixedVersion.ID
	}
	var err error
	if row.startDate, err = parseDatePtr(is.StartDate); err != nil {
		return row, fmt.Errorf("start_date: %w", err)
	}
	if row.dueDate, err = parseDatePtr(is.DueDate); err != nil {
		return row, fmt.Errorf("due_date: %w", err)
	}
	if row.createdOn, err = parseTime(is.CreatedOn); err != nil {
		return row, fmt.Errorf("created_on: %w", err)
	}
	if row.updatedOn, err = parseTime(is.UpdatedOn); err != nil {
		return row, fmt.Errorf("updated_on: %w", err)
	}
	if row.closedOn, err = parseTimePtr(is.ClosedOn); err != nil {
		return row, fmt.Errorf("closed_on: %w", err)
	}
	if row.doneRatio != nil && (*row.doneRatio < 0 || *row.doneRatio > 100) {
		return row, fmt.Errorf("done_ratio %d out of range", *row.doneRatio)
	}
	if row.estimated != nil && *row.estimated < 0 {
		return row, fmt.Errorf("negative estimated_hours")
	}
	row.hash = hashIssue(is)
	return row, nil
}

// hashIssue covers the mirrored fields. Relations are compared separately.
func hashIssue(is redmine.Issue) string {
	is.Relations = nil
	b, _ := json.Marshal(is)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (r issueRow) args(now time.Time) []any {
	return []any{r.id, r.projectID, r.parentID, r.subject, r.trackerID, r.trackerName, r.statusID, r.statusName,
		r.priorityID, r.priorityName, r.authorID, r.assigneeID, r.versionID, r.startDate, r.dueDate, r.doneRatio,
		r.estimated, r.spent, r.isPrivate, r.createdOn, r.updatedOn, r.closedOn, r.hash, now}
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", s)
	}
	return t.UTC(), nil
}

func parseTimePtr(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := parseTime(*s)
	return &t, err
}

func parseDate(s string) (time.Time, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}
	return t, nil
}

func parseDatePtr(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := parseDate(*s)
	return &t, err
}
