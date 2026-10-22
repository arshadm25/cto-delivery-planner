// Command sync-worker keeps the PostgreSQL mirror in step with Redmine.
//
// It runs a full import when there is no checkpoint, an incremental sync
// every SYNC_INTERVAL, and a full reconciliation run every
// SYNC_FULL_INTERVAL or when an incremental run finds totals that differ.
// It only sends GET requests to Redmine.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/arshadm25/cto-delivery-planner/internal/config"
	"github.com/arshadm25/cto-delivery-planner/internal/db"
	"github.com/arshadm25/cto-delivery-planner/internal/envfile"
	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
	"github.com/arshadm25/cto-delivery-planner/internal/scope"
	"github.com/arshadm25/cto-delivery-planner/internal/syncer"
)

func main() {
	envFile := flag.String("env-file", ".env", "env file to load; skipped if missing")
	once := flag.String("once", "", "run one sync (full or incremental) and exit")
	migrateOnly := flag.Bool("migrate", false, "apply database migrations and exit")
	flag.Parse()

	log := config.Logger("sync-worker")
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
	applied, err := db.Migrate(ctx, pool)
	if err != nil {
		config.Fatal(log, "migrate", err)
	}
	if len(applied) > 0 {
		log.Info("migrations applied", "versions", applied)
	}
	if *migrateOnly {
		return
	}

	sc, err := scope.LoadFile(config.String("SCOPE_FILE", "config/scope.json"))
	if err != nil {
		config.Fatal(log, "scope", err)
	}
	client, err := redmine.NewClient(redmine.Config{
		BaseURL:    os.Getenv("REDMINE_URL"),
		APIKey:     os.Getenv("REDMINE_API_KEY"),
		PageSize:   config.Int("REDMINE_PAGE_SIZE", 100),
		Timeout:    config.Duration("REDMINE_REQUEST_TIMEOUT", 30*time.Second),
		MaxRetries: config.Int("SYNC_MAX_RETRIES", 5),
	})
	if err != nil {
		config.Fatal(log, "redmine", err)
	}
	sy, err := syncer.New(client, pool, syncer.Options{
		Scope:       sc,
		Overlap:     config.Duration("SYNC_OVERLAP", 5*time.Minute),
		Concurrency: config.Int("SYNC_CONCURRENCY", 2),
		TimeEntries: config.Bool("SYNC_TIME_ENTRIES", true),
		Logger:      log,
	})
	if err != nil {
		config.Fatal(log, "syncer", err)
	}

	if *once != "" {
		kind := syncer.Kind(*once)
		if kind != syncer.Full && kind != syncer.Incremental {
			config.Fatal(log, "flag -once", fmt.Errorf("want full or incremental, got %q", *once))
		}
		res, err := sy.Run(ctx, kind)
		if err != nil {
			config.Fatal(log, "sync", err)
		}
		printSummary(res)
		if res.Outcome != "succeeded" {
			os.Exit(2)
		}
		return
	}

	w := &worker{
		sy:           sy,
		pool:         pool,
		log:          log,
		interval:     config.Duration("SYNC_INTERVAL", 30*time.Minute),
		fullInterval: config.Duration("SYNC_FULL_INTERVAL", 24*time.Hour),
		initialFull:  config.Bool("SYNC_INITIAL_FULL_IMPORT", true),
	}
	srv := &http.Server{Addr: config.String("WORKER_HEALTH_ADDR", ":8081"), Handler: w.healthHandler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health server", "error", err.Error())
		}
	}()
	w.loop(ctx)
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdown)
}

func printSummary(res *syncer.Result) {
	fmt.Printf("Run %d (%s): %s\n", res.RunID, res.Kind, res.Outcome)
	for _, k := range []string{"projects", "versions", "issues", "relations", "time_entries"} {
		if c := res.Counts[k]; c != nil {
			fmt.Printf("  %-12s fetched %d, created %d, updated %d, unchanged %d, inactivated %d, skipped %d\n",
				k, c.Fetched, c.Created, c.Updated, c.Unchanged, c.Inactivated, c.Skipped)
		}
	}
	for _, w := range res.Warnings {
		fmt.Println("  warning:", w)
	}
	for _, e := range res.Errors {
		fmt.Println("  error:", e)
	}
	if res.NeedsFull {
		fmt.Println("  Stored totals differ from Redmine; run a full sync.")
	}
}

type worker struct {
	sy           *syncer.Syncer
	pool         *pgxpool.Pool
	log          *slog.Logger
	interval     time.Duration
	fullInterval time.Duration
	initialFull  bool

	mu       sync.Mutex
	last     *syncer.Result
	lastAt   time.Time
	needFull bool
}

func (w *worker) loop(ctx context.Context) {
	lastFull := time.Time{}
	if !w.initialFull {
		lastFull = time.Now()
	}
	for {
		kind := syncer.Incremental
		w.mu.Lock()
		if w.needFull || time.Since(lastFull) >= w.fullInterval {
			kind = syncer.Full
		}
		w.mu.Unlock()
		res, err := w.sy.Run(ctx, kind)
		switch {
		case errors.Is(err, syncer.ErrAlreadyRunning):
			w.log.Info("another run holds the lock; skipping this cycle")
		case err != nil:
			w.log.Error("sync could not start", "error", err.Error())
		default:
			if res.Kind == syncer.Full && res.Outcome == "succeeded" {
				lastFull = time.Now()
			}
			w.mu.Lock()
			w.last, w.lastAt = res, time.Now()
			w.needFull = res.NeedsFull || (res.Kind == syncer.Full && res.Outcome != "succeeded")
			w.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.interval):
		}
	}
}

func (w *worker) healthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := w.pool.Ping(ctx); err != nil {
			http.Error(rw, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		rw.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /status", func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		out := map[string]any{"interval": w.interval.String(), "full_interval": w.fullInterval.String(), "full_pending": w.needFull}
		if w.last != nil {
			out["last_run"] = map[string]any{"id": w.last.RunID, "kind": w.last.Kind, "outcome": w.last.Outcome, "finished_at": w.lastAt}
		}
		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(out)
	})
	return mux
}
