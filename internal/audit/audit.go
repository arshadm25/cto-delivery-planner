// Package audit records changes to planner-owned data in audit_events.
package audit

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is a pool or a transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Actor names a user in audit rows as "user:<id> <email>".
func Actor(id int64, email string) string { return "user:" + strconv.FormatInt(id, 10) + " " + email }

// Record writes one event. before and after are stored as JSON; pass nil
// when there is no prior or new state.
func Record(ctx context.Context, q Execer, actor, action, entity, entityID string, before, after any) error {
	_, err := q.Exec(ctx, `INSERT INTO audit_events (actor, action, entity, entity_id, before, after)
		VALUES ($1, $2, $3, $4, $5, $6)`, actor, action, entity, entityID, before, after)
	return err
}
