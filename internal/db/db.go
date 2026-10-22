// Package db opens the PostgreSQL pool and applies schema migrations.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arshadm25/cto-delivery-planner/migrations"
)

// Open connects to DATABASE_URL-style connection strings and pings once.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, errors.New("db: DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// Migration is one numbered schema change.
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

var fileRE = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.(up|down)\.sql$`)

// Load reads the embedded migrations in version order.
func Load(fsys fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	byVersion := map[int]*Migration{}
	for _, e := range entries {
		m := fileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		body, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		mig := byVersion[v]
		if mig == nil {
			mig = &Migration{Version: v, Name: m[2]}
			byVersion[v] = mig
		}
		if m[3] == "up" {
			mig.Up = string(body)
		} else {
			mig.Down = string(body)
		}
	}
	var out []Migration
	for _, m := range byVersion {
		if m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("db: migration %04d needs both up and down files", m.Version)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// lockID serialises migrations across processes (API and worker both run them).
const lockID = 7_316_420_001

// Migrate applies every pending up migration, each in its own transaction.
// It returns the versions applied.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]int, error) {
	return migrateTo(ctx, pool, -1)
}

// MigrateDown rolls back to target (0 removes everything).
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, target int) ([]int, error) {
	if target < 0 {
		return nil, errors.New("db: target version must be 0 or more")
	}
	return migrateTo(ctx, pool, target)
}

func migrateTo(ctx context.Context, pool *pgxpool.Pool, target int) ([]int, error) {
	all, err := Load(migrations.FS)
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		return nil, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version integer PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	applied := map[int]bool{}
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	vs, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, err
	}
	for _, v := range vs {
		applied[v] = true
	}

	var done []int
	if target < 0 {
		for _, m := range all {
			if applied[m.Version] {
				continue
			}
			if err := runInTx(ctx, conn.Conn(), m.Up, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, m.Version, m.Name); err != nil {
				return done, fmt.Errorf("db: migration %04d_%s up: %w", m.Version, m.Name, err)
			}
			done = append(done, m.Version)
		}
		return done, nil
	}
	for i := len(all) - 1; i >= 0; i-- {
		m := all[i]
		if m.Version <= target || !applied[m.Version] {
			continue
		}
		if err := runInTx(ctx, conn.Conn(), m.Down, `DELETE FROM schema_migrations WHERE version = $1 AND name = $2`, m.Version, m.Name); err != nil {
			return done, fmt.Errorf("db: migration %04d_%s down: %w", m.Version, m.Name, err)
		}
		done = append(done, m.Version)
	}
	return done, nil
}

func runInTx(ctx context.Context, conn *pgx.Conn, body, record string, args ...any) error {
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, body); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, record, args...)
		return err
	})
}
