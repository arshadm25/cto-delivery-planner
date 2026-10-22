// Command devseed fills a development database with fictional data from
// the in-memory fake Redmine and creates demo accounts. It refuses to run
// unless APP_ENV=development, so it cannot touch a real deployment.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/db"
	"github.com/arshadm25/cto-delivery-planner/internal/envfile"
	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
	"github.com/arshadm25/cto-delivery-planner/internal/redminetest"
	"github.com/arshadm25/cto-delivery-planner/internal/scope"
	"github.com/arshadm25/cto-delivery-planner/internal/syncer"
)

// DemoPassword is used for every demo account.
const DemoPassword = "demo-password-123"

func main() {
	envfile.Load(".env")
	if os.Getenv("APP_ENV") != "development" {
		fmt.Fprintln(os.Stderr, "devseed: set APP_ENV=development; this command writes fictional data")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	if _, err := db.Migrate(ctx, pool); err != nil {
		fail(err)
	}

	fake := redminetest.New()
	defer fake.Close()
	fake.Seed(24)
	// Make the demo data show a range of states relative to today.
	today := time.Now().UTC()
	d := func(days int) *string { s := today.AddDate(0, 0, days).Format(time.DateOnly); return &s }
	recent := today.Add(-2 * time.Hour).Format(time.RFC3339)
	fake.Lock()
	fake.Versions[0].EffectiveDate = d(10)
	fake.Versions[1].EffectiveDate = d(-3)
	for i := range fake.Issues {
		is := &fake.Issues[i]
		if i%5 != 0 {
			is.UpdatedOn = recent
		}
		switch i % 4 {
		case 0:
			is.DueDate = d(-5)
		case 1:
			is.DueDate = d(20)
		}
	}
	fake.Issues[1].Priority = redmine.IDName{ID: 3, Name: "High"}
	fake.Unlock()

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	sy, err := syncer.New(fake.Client(), pool, syncer.Options{Scope: &scope.Scope{ProjectIDs: []int{10, 11}}, TimeEntries: true, Logger: quiet})
	if err != nil {
		fail(err)
	}
	res, err := sy.Run(ctx, syncer.Full)
	if err != nil {
		fail(err)
	}
	fmt.Printf("Demo sync %s: %d issues\n", res.Outcome, res.Counts["issues"].Fetched)

	as := auth.NewStore(pool)
	for _, u := range []struct {
		email string
		role  auth.Role
		proj  []int
	}{
		{"admin@example.test", auth.Admin, nil},
		{"cto@example.test", auth.CTO, nil},
		{"pm@example.test", auth.ProjectManager, []int{10}},
		{"viewer@example.test", auth.Viewer, []int{11}},
	} {
		created, err := as.CreateUser(ctx, u.email, "Demo "+string(u.role), DemoPassword, u.role)
		if err == auth.ErrDuplicate {
			continue
		}
		if err != nil {
			fail(err)
		}
		if u.proj != nil {
			as.SetAssignedProjects(ctx, created.ID, u.proj)
		}
	}
	fmt.Printf("Demo accounts: admin@, cto@, pm@ and viewer@example.test, password %s\n", DemoPassword)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "devseed:", err)
	os.Exit(1)
}
