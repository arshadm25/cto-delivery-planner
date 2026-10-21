// Command redmine-discovery reads projects and issues from Redmine and writes
// the Phase 1 baseline report. It only sends GET requests.
//
// The Redmine URL and API key come from REDMINE_URL and REDMINE_API_KEY.
// The key is never printed.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/discovery"
	"github.com/arshadm25/cto-delivery-planner/internal/redmine"
)

func main() {
	out := flag.String("out", "discovery-out", "directory for report.md, projects.csv and issue-index.csv")
	pageSize := flag.Int("page-size", envInt("REDMINE_PAGE_SIZE", 100), "requested page size; the server may cap it")
	timeout := flag.Duration("timeout", envDuration("REDMINE_REQUEST_TIMEOUT", 30*time.Second), "per-request timeout")
	retries := flag.Int("retries", 3, "retries for network errors, HTTP 429 and 5xx")
	authOnly := flag.Bool("auth-only", false, "only test authentication against /users/current.json")
	flag.Parse()

	client, err := redmine.NewClient(redmine.Config{
		BaseURL:    os.Getenv("REDMINE_URL"),
		APIKey:     os.Getenv("REDMINE_API_KEY"),
		PageSize:   *pageSize,
		Timeout:    *timeout,
		MaxRetries: *retries,
	})
	if err != nil {
		fail("%v (set REDMINE_URL and REDMINE_API_KEY in the environment)", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *authOnly {
		u, err := client.CurrentUser(ctx)
		if err != nil {
			fail("authentication failed: %v", err)
		}
		fmt.Printf("Authenticated as %q (user ID %d, administrator: %t)\n", u.Login, u.ID, u.Admin)
		return
	}

	start := time.Now()
	r, err := discovery.Run(ctx, client, start)
	if err != nil {
		fail("%v", err)
	}
	if err := discovery.Write(r, *out); err != nil {
		fail("%v", err)
	}
	fmt.Printf("Fetched %d projects and %d issues in %s.\n", len(r.Projects), len(r.Issues), time.Since(start).Round(time.Second))
	for _, w := range r.Warnings {
		fmt.Println("Warning:", w)
	}
	fmt.Printf("Report written to %s/report.md\n", *out)
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "redmine-discovery: "+format+"\n", a...)
	os.Exit(1)
}

func envInt(name string, def int) int {
	var n int
	if _, err := fmt.Sscan(os.Getenv(name), &n); err == nil && n > 0 {
		return n
	}
	return def
}

func envDuration(name string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(name)); err == nil && d > 0 {
		return d
	}
	return def
}
