#!/usr/bin/env bash
# Nightly PostgreSQL backup (P5-04). Writes a compressed custom-format dump
# with a SHA-256 checksum and deletes dumps older than BACKUP_KEEP_DAYS.
#
#   BACKUP_DIR=/var/backups/cto-planner deploy/scripts/backup.sh
#
# Cron example (02:30 every night):
#   30 2 * * * cd /opt/cto-delivery-planner && BACKUP_DIR=/var/backups/cto-planner deploy/scripts/backup.sh >> /var/log/cto-planner-backup.log 2>&1
#
# Without Docker (PostgreSQL installed on the host), set PG_DIRECT=1. The
# tools then connect with the usual libpq variables (PGHOST, PGPORT, PGUSER,
# PGPASSWORD or ~/.pgpass) to the database named in PLANNER_DB:
#   PG_DIRECT=1 PGHOST=localhost PGUSER=planner BACKUP_DIR=... deploy/scripts/backup.sh
#
# Copy the backup directory off the host as well; a backup on the same
# disk does not survive losing that disk.
set -euo pipefail

BACKUP_DIR=${BACKUP_DIR:-/var/backups/cto-planner}
BACKUP_KEEP_DAYS=${BACKUP_KEEP_DAYS:-14}
COMPOSE=${COMPOSE:-docker compose -f deploy/docker-compose.yml}
PG_DIRECT=${PG_DIRECT:-0}
PLANNER_DB=${PLANNER_DB:-cto_planner}

# pg runs a PostgreSQL client tool inside the postgres container, or on the
# host when PG_DIRECT=1.
pg() {
  if [ "$PG_DIRECT" = 1 ]; then command "$@"; else $COMPOSE exec -T postgres "$1" -U planner "${@:2}"; fi
}
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }

umask 077
mkdir -p "$BACKUP_DIR"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
out="$BACKUP_DIR/cto_planner-$stamp.dump"

pg pg_dump -d "$PLANNER_DB" --format=custom --compress=9 > "$out.partial"
# A dump that pg_restore cannot list is not a backup.
pg pg_restore --list < "$out.partial" > /dev/null
mv "$out.partial" "$out"
sha256 "$out" > "$out.sha256"
echo "$(date -u +%FT%TZ) backup written: $out ($(du -h "$out" | cut -f1))"

find "$BACKUP_DIR" -name 'cto_planner-*.dump*' -mtime +"$BACKUP_KEEP_DAYS" -print -delete
