package db_test

import (
	"context"
	"testing"

	"github.com/arshadm25/cto-delivery-planner/internal/db"
	"github.com/arshadm25/cto-delivery-planner/internal/testdb"
	"github.com/arshadm25/cto-delivery-planner/migrations"
)

func TestLoadPairsUpAndDown(t *testing.T) {
	ms, err := db.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].Version != 1 {
		t.Fatalf("got %+v", ms)
	}
	for i := 1; i < len(ms); i++ {
		if ms[i].Version <= ms[i-1].Version {
			t.Fatal("not sorted")
		}
	}
}

func TestMigrateDownAndUpAgain(t *testing.T) {
	pool := testdb.New(t) // already migrated up
	ctx := context.Background()
	if again, err := db.Migrate(ctx, pool); err != nil || len(again) != 0 {
		t.Fatalf("second migrate applied %v, err %v", again, err)
	}
	all, _ := db.Load(migrations.FS)
	down, err := db.MigrateDown(ctx, pool, 0)
	if err != nil || len(down) != len(all) {
		t.Fatalf("down %v err %v", down, err)
	}
	var n int
	pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'schema_migrations'`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d tables left after down", n)
	}
	if up, err := db.Migrate(ctx, pool); err != nil || len(up) != len(all) {
		t.Fatalf("up %v err %v", up, err)
	}
}
