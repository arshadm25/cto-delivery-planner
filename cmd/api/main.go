// Command api serves the planner REST API under /api/v1.
//
// It reads the PostgreSQL mirror and writes only planner-owned tables. It
// has no Redmine credentials.
//
// Create the first account with:
//
//	PLANNER_NEW_PASSWORD=... api -create-user admin@example.com -name "Admin" -role admin
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/api"
	"github.com/arshadm25/cto-delivery-planner/internal/auth"
	"github.com/arshadm25/cto-delivery-planner/internal/config"
	"github.com/arshadm25/cto-delivery-planner/internal/db"
	"github.com/arshadm25/cto-delivery-planner/internal/envfile"
	"github.com/arshadm25/cto-delivery-planner/internal/store"
)

func main() {
	envFile := flag.String("env-file", ".env", "env file to load; skipped if missing")
	createUser := flag.String("create-user", "", "create an account with this email and exit")
	name := flag.String("name", "", "display name for -create-user")
	role := flag.String("role", "viewer", "role for -create-user: admin, cto, project_manager, tech_lead or viewer")
	flag.Parse()

	log := config.Logger("api")
	if _, err := envfile.Load(*envFile); err != nil {
		config.Fatal(log, "env file", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		config.Fatal(log, "database", err)
	}
	defer pool.Close()
	if _, err := db.Migrate(ctx, pool); err != nil {
		config.Fatal(log, "migrate", err)
	}
	as := auth.NewStore(pool)

	if *createUser != "" {
		pw := os.Getenv("PLANNER_NEW_PASSWORD")
		if pw == "" {
			fmt.Fprint(os.Stderr, "Password (input is visible; prefer PLANNER_NEW_PASSWORD): ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			pw = strings.TrimRight(line, "\r\n")
		}
		if *name == "" {
			*name = *createUser
		}
		u, err := as.CreateUser(ctx, *createUser, *name, pw, auth.Role(*role))
		if err != nil {
			config.Fatal(log, "create user", err)
		}
		fmt.Printf("Created %s (id %d, role %s)\n", u.Email, u.ID, u.Role)
		return
	}

	loc, err := time.LoadLocation(config.String("APP_TIMEZONE", "UTC"))
	if err != nil {
		config.Fatal(log, "APP_TIMEZONE", err)
	}
	srv := api.New(api.Config{
		CookieSecure:   config.Bool("COOKIE_SECURE", true),
		SessionTTL:     config.Duration("SESSION_TTL", 12*time.Hour),
		RedmineURL:     config.String("REDMINE_PUBLIC_URL", os.Getenv("REDMINE_URL")),
		Location:       loc,
		SyncStaleAfter: config.Duration("SYNC_STALE_AFTER", 2*time.Hour),
	}, store.New(pool), as, log)

	go srv.RunSnapshotLoop(ctx, config.Int("SNAPSHOT_RETENTION_DAYS", 730), log)

	hs := &http.Server{
		Addr:              config.String("API_ADDR", ":8080"),
		Handler:           srv,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sh, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hs.Shutdown(sh)
	}()
	log.Info("listening", "addr", hs.Addr)
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		config.Fatal(log, "listen", err)
	}
}
