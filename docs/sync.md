# Sync worker and database

Backlog items P2-01 to P2-09. The sync worker copies Redmine data into PostgreSQL. It only sends GET requests, and it only reads the projects listed in [`config/scope.json`](../config/scope.json).

## Run it locally

```sh
make up                 # PostgreSQL 16 in Docker, bound to 127.0.0.1:5432
cp .env.example .env    # then fill in REDMINE_URL and REDMINE_API_KEY on the trusted host
make migrate            # create the tables
make sync-full          # first full import; prints counts per resource
make sync-incremental   # changes since the last checkpoint
./bin/sync-worker       # keep running on SYNC_INTERVAL, health on :8081
```

`make test-integration` runs the database tests against the local server. CI runs them against a Postgres service container.

## What gets copied

| Table | Source | Notes |
|---|---|---|
| `redmine_projects` | `/projects.json` | All visible projects; `in_scope` marks the scope file's IDs |
| `redmine_statuses` | `/issue_statuses.json` | Used to tell open from closed |
| `redmine_versions` | `/projects/{id}/versions.json` | Milestones, including versions shared into a project |
| `redmine_issues` | `/issues.json?status_id=*&include=relations` | Open and closed. No description is copied |
| `redmine_issue_relations` | `include=relations` on issues | Blocks, precedes, relates and so on |
| `redmine_users` | Author and assignee on issues and time entries | ID and display name only. `/users.json` needs an admin key, so it is not used |
| `redmine_time_entries` | `/time_entries.json` | Skipped with a warning if the account may not view them. Comments are not copied |

Missing values stay `NULL`. An unestimated issue has `estimated_hours IS NULL`, never `0`.

Each in-scope project is queried with `subproject_id=!*`, because every subproject is listed in scope on its own.

## Full and incremental runs

- **Full run.** Reads every in-scope project. Each project is written in one transaction: users, versions, issues, relations, time entries, then a check that the stored active issue count equals what the API returned. Issues, versions and time entries that Redmine no longer returns get `inactive_since` set. Nothing is deleted except relations that no longer exist.
- **Incremental run.** Reads issues with `updated_on >= checkpoint - SYNC_OVERLAP` across all projects and upserts the in-scope ones. Projects and versions are refreshed every time. Then it compares stored totals with `total_count` per project. A deleted or moved issue does not change `updated_on`, so a mismatch makes the worker run a full sync next.
- **Checkpoint.** The newest `updated_on` stored, which comes from Redmine's own clock. It advances only when every project committed and validated. A partial or failed run leaves it alone, and the previous data stays in place.
- **Interrupted runs.** A Postgres advisory lock allows one run at a time. A run left as `running` by a stopped process is marked `abandoned` by the next run, which starts again. Writes are idempotent, so nothing is duplicated.
- **Schedule.** Full when there is no checkpoint, every `SYNC_FULL_INTERVAL`, after a failed full run, or when reconciliation found a difference; incremental otherwise.

Each run is recorded in `sync_runs` with counts per resource (fetched, created, updated, unchanged, inactivated, skipped), the checkpoint before and after, warnings and an error summary. `sync_reconciliation` holds the per-project totals.

## To confirm on your Redmine (Q2)

The `updated_on=>=<timestamp>` filter and `include=relations` on the issue list are documented Redmine API features, but the installed version is not recorded yet. Run `make sync-full` and then `make sync-incremental` once and check that the second run reports a small number of fetched issues and no reconciliation mismatch.

## Integration tests (Q11)

The tests use an in-memory fake Redmine (`internal/redminetest`) with fictional data, not a Redmine container. They cover multi-page import, closed issues, parent and relation mapping, null estimates, idempotent re-import, incremental sync inside the overlap window, deletion found by reconciliation, retry after transient errors, a failing project keeping its old data and the checkpoint, rollback of an invalid batch, resume after an interrupted run, refused time entries and the single-run lock. Testing against a Redmine container matched to your version is still open under Q11.
