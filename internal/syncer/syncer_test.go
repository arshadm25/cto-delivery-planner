package syncer

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
	"github.com/arshadm25/cto-delivery-planner/internal/redminetest"
	"github.com/arshadm25/cto-delivery-planner/internal/scope"
	"github.com/arshadm25/cto-delivery-planner/internal/testdb"
)

func setup(t *testing.T, perProject int) (*redminetest.Server, *pgxpool.Pool, *Syncer) {
	t.Helper()
	pool := testdb.New(t)
	srv := redminetest.New()
	t.Cleanup(srv.Close)
	srv.Seed(perProject)
	sy, err := New(srv.Client(), pool, Options{
		Scope:       &scope.Scope{ProjectIDs: []int{10, 11}},
		Overlap:     5 * time.Minute,
		Concurrency: 2,
		TimeEntries: true,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, pool, sy
}

func mustRun(t *testing.T, sy *Syncer, kind Kind) *Result {
	t.Helper()
	res, err := sy.Run(context.Background(), kind)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestFullImportMultiPageWithClosedIssues(t *testing.T) {
	srv, pool, sy := setup(t, 60) // 60 per project, server caps pages at 25
	res := mustRun(t, sy, Full)
	if res.Outcome != "succeeded" {
		t.Fatalf("outcome %s errors %v", res.Outcome, res.Errors)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE inactive_since IS NULL`); got != 120 {
		t.Fatalf("stored %d issues, want 120 (out-of-scope project excluded)", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE closed_on IS NOT NULL`); got != 40 {
		t.Fatalf("closed %d, want 40", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE estimated_hours IS NULL`); got != 60 {
		t.Fatalf("null estimates %d, want 60: missing values must stay null", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE redmine_id = 1002 AND parent_redmine_id = 1001`); got != 1 {
		t.Fatal("parent not mapped")
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issue_relations WHERE issue_from_redmine_id = 1001 AND issue_to_redmine_id = 1002 AND relation_type = 'blocks'`); got != 1 {
		t.Fatal("relation not mapped")
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_time_entries`); got != 1 {
		t.Fatalf("time entries %d", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_projects WHERE in_scope`); got != 2 {
		t.Fatalf("in-scope projects %d", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM sync_reconciliation WHERE run_id = $1 AND matches`, res.RunID); got != 2 {
		t.Fatalf("reconciliation rows %d", got)
	}
	if res.CheckpointAfter == nil {
		t.Fatal("checkpoint not set")
	}
	if len(srv.Methods) != 0 {
		t.Fatalf("non-GET requests sent: %v", srv.Methods)
	}
	if res.Counts["issues"].Created != 120 {
		t.Fatalf("counts %+v", res.Counts["issues"])
	}
}

func TestRepeatImportIsIdempotent(t *testing.T) {
	_, pool, sy := setup(t, 30)
	mustRun(t, sy, Full)
	res := mustRun(t, sy, Full)
	c := res.Counts["issues"]
	if c.Created != 0 || c.Updated != 0 || c.Unchanged != 60 {
		t.Fatalf("second run counts %+v, want all unchanged", c)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues`); got != 60 {
		t.Fatalf("rows %d after two runs", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issue_relations`); got != 1 {
		t.Fatalf("relations %d after two runs", got)
	}
}

func TestIncrementalWithOverlapAndReconciliation(t *testing.T) {
	srv, pool, sy := setup(t, 10)
	first := mustRun(t, sy, Full)

	// An update one minute after the checkpoint, inside the overlap window
	// on the next run.
	srv.Touch(1003, "2026-01-05T09:01:00Z", func(is *redmine.Issue) { is.Subject = "Renamed fictional task" })
	res := mustRun(t, sy, Incremental)
	if res.Kind != Incremental || res.Outcome != "succeeded" {
		t.Fatalf("kind %s outcome %s errors %v", res.Kind, res.Outcome, res.Errors)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE subject = 'Renamed fictional task'`); got != 1 {
		t.Fatal("update not applied")
	}
	if !res.CheckpointAfter.After(*first.CheckpointAfter) {
		t.Fatalf("checkpoint did not advance: %v -> %v", first.CheckpointAfter, res.CheckpointAfter)
	}
	if res.NeedsFull {
		t.Fatal("unexpected reconciliation mismatch")
	}

	// A deleted issue is invisible to updated_on; reconciliation catches it.
	srv.Delete(1004)
	res = mustRun(t, sy, Incremental)
	if !res.NeedsFull {
		t.Fatal("deletion not detected")
	}
	res = mustRun(t, sy, Full)
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE redmine_id = 1004 AND inactive_since IS NOT NULL`); got != 1 {
		t.Fatal("deleted issue not marked inactive")
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE redmine_id = 1004`); got != 1 {
		t.Fatal("inactive issue must not be deleted")
	}
}

func TestRetryAfterTransientFailure(t *testing.T) {
	srv, _, sy := setup(t, 5)
	srv.Lock()
	srv.FailNext = 2
	srv.Unlock()
	res := mustRun(t, sy, Full)
	if res.Outcome != "succeeded" {
		t.Fatalf("outcome %s errors %v", res.Outcome, res.Errors)
	}
}

func TestProjectFailureKeepsPreviousDataAndCheckpoint(t *testing.T) {
	srv, pool, sy := setup(t, 5)
	first := mustRun(t, sy, Full)
	srv.Touch(1001, "2026-02-01T00:00:00Z", func(is *redmine.Issue) { is.Subject = "Changed while project 11 is failing" })
	srv.Lock()
	srv.FailProject = 11
	srv.Unlock()
	res := mustRun(t, sy, Full)
	if res.Outcome != "partial" {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE project_redmine_id = 11 AND inactive_since IS NULL`); got != 5 {
		t.Fatalf("project 11 lost data: %d", got)
	}
	var cp time.Time
	pool.QueryRow(context.Background(), `SELECT value FROM sync_checkpoints WHERE name = $1`, CheckpointIssues).Scan(&cp)
	if !cp.Equal(*first.CheckpointAfter) {
		t.Fatalf("checkpoint moved on a partial run: %v", cp)
	}
	var summary string
	pool.QueryRow(context.Background(), `SELECT error_summary FROM sync_runs WHERE id = $1`, res.RunID).Scan(&summary)
	if summary == "" {
		t.Fatal("error not recorded")
	}
}

func TestInvalidBatchRollsBack(t *testing.T) {
	srv, pool, sy := setup(t, 5)
	srv.Touch(1002, "not-a-time", nil)
	res := mustRun(t, sy, Full)
	if res.Outcome != "partial" {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE project_redmine_id = 10`); got != 0 {
		t.Fatalf("project 10 partly written: %d rows", got)
	}
	if got := count(t, pool, `SELECT count(*) FROM redmine_issues WHERE project_redmine_id = 11`); got != 5 {
		t.Fatalf("project 11 rows %d", got)
	}
	if res.Counts["issues"].Created != 5 {
		t.Fatalf("rolled-back rows counted: %+v", res.Counts["issues"])
	}
}

func TestInterruptedRunIsAbandonedAndResumed(t *testing.T) {
	_, pool, sy := setup(t, 5)
	ctx := context.Background()
	var stale int64
	pool.QueryRow(ctx, `INSERT INTO sync_runs (kind, started_at, heartbeat_at, outcome) VALUES ('full', now(), now(), 'running') RETURNING id`).Scan(&stale)
	res := mustRun(t, sy, Incremental) // no checkpoint yet, so it becomes full
	if res.Kind != Full || res.Outcome != "succeeded" {
		t.Fatalf("kind %s outcome %s", res.Kind, res.Outcome)
	}
	var outcome string
	pool.QueryRow(ctx, `SELECT outcome FROM sync_runs WHERE id = $1`, stale).Scan(&outcome)
	if outcome != "abandoned" {
		t.Fatalf("stale run is %s", outcome)
	}
}

func TestTimeEntriesForbiddenIsAWarning(t *testing.T) {
	srv, _, sy := setup(t, 3)
	srv.Lock()
	srv.TimeEntriesForbidden = true
	srv.Unlock()
	res := mustRun(t, sy, Full)
	if res.Outcome != "succeeded" || len(res.Warnings) == 0 {
		t.Fatalf("outcome %s warnings %v", res.Outcome, res.Warnings)
	}
}

func TestConcurrentRunIsRefused(t *testing.T) {
	_, pool, sy := setup(t, 1)
	ctx := context.Background()
	conn, _ := pool.Acquire(ctx)
	defer conn.Release()
	conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, runLockID)
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, runLockID)
	if _, err := sy.Run(ctx, Full); err != ErrAlreadyRunning {
		t.Fatalf("err %v", err)
	}
}
