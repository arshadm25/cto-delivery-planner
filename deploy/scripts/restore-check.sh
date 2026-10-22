#!/usr/bin/env bash
# Restore test (P5-04). Restores a dump into a scratch database next to the
# live one, compares row counts table by table, then drops the scratch
# database. The live database is only read.
#
#   deploy/scripts/restore-check.sh /var/backups/cto-planner/cto_planner-20261022T023000Z.dump
#
# Counts can differ by the rows written since the dump was taken; sync
# tables change every SYNC_INTERVAL.
set -euo pipefail

dump=${1:?usage: restore-check.sh DUMP_FILE}
COMPOSE=${COMPOSE:-docker compose -f deploy/docker-compose.yml}
scratch=restore_check_$(date -u +%Y%m%d%H%M%S)

if [ -f "$dump.sha256" ]; then
  sha256sum --check --status "$dump.sha256" || { echo "checksum mismatch for $dump" >&2; exit 1; }
fi

psql() { $COMPOSE exec -T postgres psql -U planner -v ON_ERROR_STOP=1 -qAt "$@"; }
cleanup() { psql -d postgres -c "DROP DATABASE IF EXISTS $scratch" >/dev/null || true; }
trap cleanup EXIT

psql -d postgres -c "CREATE DATABASE $scratch"
$COMPOSE exec -T postgres pg_restore -U planner -d "$scratch" --no-owner --exit-on-error < "$dump"

tables="redmine_projects redmine_issues redmine_versions redmine_issue_relations redmine_time_entries redmine_users
sync_runs app_users delivery_commitments capacity_allocations project_snapshots audit_events schema_migrations"
printf '%-26s %10s %10s\n' table restored live
for t in $tables; do
  r=$(psql -d "$scratch" -c "SELECT count(*) FROM $t" 2>/dev/null || echo missing)
  l=$(psql -d cto_planner -c "SELECT count(*) FROM $t" 2>/dev/null || echo missing)
  printf '%-26s %10s %10s\n' "$t" "$r" "$l"
done
echo "Restore succeeded into $scratch; it is dropped on exit."
