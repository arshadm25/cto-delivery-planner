#!/usr/bin/env bash
# Restore test (P5-04). Restores a dump into a scratch database next to the
# live one, compares row counts table by table, then drops the scratch
# database. The live database is only read.
#
#   deploy/scripts/restore-check.sh /var/backups/cto-planner/cto_planner-20261022T023000Z.dump
#
# Without Docker, set PG_DIRECT=1 and the libpq variables (PGHOST, PGUSER,
# PGPASSWORD or ~/.pgpass), as for backup.sh. That user needs CREATEDB.
#
# Counts can differ by the rows written since the dump was taken; sync
# tables change every SYNC_INTERVAL.
set -euo pipefail

dump=${1:?usage: restore-check.sh DUMP_FILE}
COMPOSE=${COMPOSE:-docker compose -f deploy/docker-compose.yml}
PG_DIRECT=${PG_DIRECT:-0}
PLANNER_DB=${PLANNER_DB:-cto_planner}
scratch=restore_check_$(date -u +%Y%m%d%H%M%S)

if [ -f "$dump.sha256" ]; then
  # The .sha256 file holds the path as backup.sh wrote it, so check from there.
  if command -v sha256sum >/dev/null; then sum=(sha256sum --check --status); else sum=(shasum -a 256 --check --status); fi
  "${sum[@]}" "$dump.sha256" || { echo "checksum mismatch for $dump" >&2; exit 1; }
fi

pg() {
  if [ "$PG_DIRECT" = 1 ]; then command "$@"; else $COMPOSE exec -T postgres "$1" -U planner "${@:2}"; fi
}
psql() { pg psql -v ON_ERROR_STOP=1 -qAt "$@"; }
cleanup() { psql -d postgres -c "DROP DATABASE IF EXISTS $scratch" >/dev/null || true; }
trap cleanup EXIT

psql -d postgres -c "CREATE DATABASE $scratch"
pg pg_restore -d "$scratch" --no-owner --exit-on-error < "$dump"

tables="redmine_projects redmine_issues redmine_versions redmine_issue_relations redmine_time_entries redmine_users
sync_runs app_users app_user_projects project_roles planner_settings delivery_commitments commitment_scope
capacity_allocations project_snapshots issue_plans planner_milestones planner_tasks planner_teams planner_team_members
audit_events schema_migrations"
printf '%-26s %10s %10s\n' table restored live
for t in $tables; do
  r=$(psql -d "$scratch" -c "SELECT count(*) FROM $t" 2>/dev/null || echo missing)
  l=$(psql -d "$PLANNER_DB" -c "SELECT count(*) FROM $t" 2>/dev/null || echo missing)
  printf '%-26s %10s %10s\n' "$t" "$r" "$l"
done
echo "Restore succeeded into $scratch; it is dropped on exit."
