# Operations runbook and user guide

This covers deploying and running the planner on one trusted host with Docker Compose, and how to read each dashboard view. Values in angle brackets are placeholders.

## 1. Layout

| Container | Network | Purpose |
|---|---|---|
| `postgres` | private only | PostgreSQL 16. No published port. |
| `api` | private only | REST API, login, snapshots. Runs migrations on start. |
| `worker` | private + egress | The only container that can reach Redmine. Runs migrations on start. |
| `web` | private + edge | Next.js dashboard, published on `127.0.0.1:${WEB_PORT:-3000}` only. |

The `private` network is internal, so PostgreSQL and the API cannot be reached from outside the host. Put a TLS reverse proxy in front of the web port (examples in `deploy/reverse-proxy/`). Session cookies are `Secure`, so the dashboard only works over HTTPS in production.

All Go containers run as a non-root user with a read-only filesystem, no capabilities and `no-new-privileges`.

## 2. First deploy

1. Clone the repo to the host, for example `/opt/cto-delivery-planner`.
2. Create the secrets file. It must stay outside the repo and be readable only by root:
   ```bash
   sudo mkdir -p /etc/cto-planner
   sudo install -m 600 -o root -g root deploy/planner.env.example /etc/cto-planner/planner.env
   sudoedit /etc/cto-planner/planner.env
   ```
   Fill in `REDMINE_URL`, `REDMINE_API_KEY` (the read-only service account, see [scope.md](scope.md)), `POSTGRES_PASSWORD`, and `DATABASE_URL` with the same password. Set `APP_TIMEZONE` to the team's zone (for example `Asia/Kolkata`); it decides what "today" means for overdue and stale checks.
3. Check `config/scope.json` lists the agreed project IDs. The worker mounts it read-only, so later changes need only a worker restart.
4. Build and start:
   ```bash
   make deploy-build
   make deploy-up
   docker compose -f deploy/docker-compose.yml ps     # all four should become healthy
   ```
5. Create the first admin. The password is read from the environment so it does not land in shell history if you use `read -s`:
   ```bash
   read -rs PLANNER_NEW_PASSWORD && export PLANNER_NEW_PASSWORD
   docker compose -f deploy/docker-compose.yml run --rm -e PLANNER_NEW_PASSWORD \
     api -create-user <admin@company> -name "<Admin name>" -role admin
   unset PLANNER_NEW_PASSWORD
   ```
   Passwords need at least 12 characters. Then sign in and create the other accounts under **Admin → Users**, assigning projects to project managers, tech leads and viewers. Admin and CTO accounts see every project.
6. The worker runs a full import on first start. Follow it with `docker compose -f deploy/docker-compose.yml logs -f worker` or the **Sync** page.
7. Install the cron jobs in sections 5 and 6.

## 3. Day to day

| Task | Command or place |
|---|---|
| See sync state | **Sync** page, or `docker compose -f deploy/docker-compose.yml exec worker /app/probe http://localhost:8081/status` |
| Force a full re-import | `docker compose -f deploy/docker-compose.yml run --rm --entrypoint /app/sync-worker worker -env-file /dev/null -once full` |
| Restart after a config change | `make deploy-up` (Compose recreates changed containers) |
| Logs | `docker compose -f deploy/docker-compose.yml logs --since 1h <service>` (JSON, 5 × 20 MB per container) |
| Stop everything | `docker compose -f deploy/docker-compose.yml down` (data stays in the `pgdata` volume) |

A full re-import is safe to repeat: rows are updated in place, never duplicated, and records Redmine no longer returns are marked inactive rather than deleted.

## 4. Rotating the Redmine API key

1. Create the new key on the service account in Redmine.
2. Replace `REDMINE_API_KEY` in `/etc/cto-planner/planner.env`.
3. `docker compose -f deploy/docker-compose.yml up -d --force-recreate worker`.
4. Confirm the next run on the **Sync** page succeeds, then revoke the old key in Redmine.

Only the worker uses the key. It is never sent to the browser, and the logger replaces its value with `[REDACTED]` if it ever appears in a log line.

## 5. Backups and restore

Nightly backup (custom-format `pg_dump`, checked with `pg_restore --list`, with a SHA-256 file, older dumps removed after `BACKUP_KEEP_DAYS`, default 14):

```cron
30 2 * * * cd /opt/cto-delivery-planner && BACKUP_DIR=/var/backups/cto-planner deploy/scripts/backup.sh >> /var/log/cto-planner-backup.log 2>&1
```

Copy `/var/backups/cto-planner` off the host as well; a backup on the same disk does not survive losing the disk. The dumps hold Redmine issue data and planner accounts, so keep the copy access-controlled.

Monthly restore test, which restores the latest dump into a scratch database, compares row counts with the live database and drops the scratch copy:

```bash
deploy/scripts/restore-check.sh /var/backups/cto-planner/<latest>.dump
```

Real restore after data loss:

```bash
docker compose -f deploy/docker-compose.yml stop api worker web
docker compose -f deploy/docker-compose.yml exec -T postgres \
  pg_restore -U planner -d cto_planner --clean --if-exists < /var/backups/cto-planner/<file>.dump
make deploy-up
```

Redmine mirror tables can always be rebuilt by a full import. Planner-owned data (accounts, commitments, baselines, capacity, settings, snapshots, audit log) exists only in the database and its backups.

## 6. Health checks and alerts

Endpoints (all inside the private network):

| Endpoint | Meaning |
|---|---|
| api `/healthz` | Process is up |
| api `/readyz` | Database reachable |
| api `/health/sync` | 200 when the latest run succeeded and the last success is newer than `SYNC_STALE_AFTER`; 503 otherwise. No login needed, no project data returned. |
| worker `/readyz`, `/status` | Worker up; last run summary |

Alert job, every 15 minutes. It posts `{"text": "..."}` to `ALERT_WEBHOOK_URL` (a Slack or Teams incoming webhook works) and exits 1 when anything fails. The channel is still to be decided.

```cron
*/15 * * * * cd /opt/cto-delivery-planner && ALERT_WEBHOOK_URL=<webhook> deploy/scripts/check-health.sh >> /var/log/cto-planner-health.log 2>&1
```

When an alert fires:

1. Open the **Sync** page. The error summary says which project failed and why.
2. 401 or 403 from Redmine: the key was revoked or the service account lost access; see section 4.
3. Network errors: check the host can reach `REDMINE_URL`.
4. A `partial` run keeps the previous data for the failed project; the dashboard shows a banner saying so until a run succeeds.

## 7. Upgrades

```bash
cd /opt/cto-delivery-planner
deploy/scripts/backup.sh            # always back up first
git pull
make deploy-build && make deploy-up
```

Migrations run automatically and are recorded in `schema_migrations`. Each migration has a down file; rolling back is a manual step (`db.MigrateDown`) and should be done from a backup restore instead where possible. To pin a version, set `PLANNER_VERSION=<tag>` before building.

## 8. Reading the dashboard

**Overview.** One card per in-scope project with health, open issues, overdue count, highest flags and the delivery forecast. Health is *blocked*, *at risk*, *on track* or *insufficient data*. Insufficient data means there are too few estimates or dates to judge; it is not a pass.

**Project page.** Milestones (Redmine versions) with due date, progress, the approved baseline and the forecast side by side, the issue list with filters, and data gaps such as unestimated issues. Owner and tech lead are recorded in the planner and show *TBD* until someone with permission enters and verifies them. Every issue links back to Redmine.

**Risks.** Every open flag across visible projects, highest severity first, each with the evidence that triggered it and a suggested action. Thresholds (stale after N days, milestone warning window, high priorities, release keywords) are under **Settings**.

**Baseline, forecast, actual.** These are separate on purpose.
- *Baseline* is a commitment proposed by a project manager and approved by the CTO. It changes only through a new approved commitment, and every change is in the audit log.
- *Forecast* is calculated: remaining estimated effort over approved capacity, or the recent close rate when capacity is missing, otherwise *TBD*. Each forecast lists its method, inputs, assumptions and confidence. See [forecasting.md](forecasting.md).
- *Actual* is the date the commitment was marked delivered.

**Capacity and workload.** Allocations are drafts until approved, and only approved allocations feed forecasts. Workload compares assigned open estimates with allocated hours per person and flags over-allocation and unassigned work.

**Weekly report.** Changes since the previous week's snapshot. The Markdown export masks people's names unless an authorised user explicitly asks for full names; check the export before sharing it outside the team.

**Sync.** Recent runs with counts, warnings and errors, and the time of the last successful sync. If the latest run failed, every page shows that it is showing earlier data.
