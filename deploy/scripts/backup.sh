#!/usr/bin/env bash
# Nightly PostgreSQL backup (P5-04). Writes a compressed custom-format dump
# with a SHA-256 checksum and deletes dumps older than BACKUP_KEEP_DAYS.
#
#   BACKUP_DIR=/var/backups/cto-planner deploy/scripts/backup.sh
#
# Cron example (02:30 every night):
#   30 2 * * * cd /opt/cto-delivery-planner && BACKUP_DIR=/var/backups/cto-planner deploy/scripts/backup.sh >> /var/log/cto-planner-backup.log 2>&1
#
# Copy the backup directory off the host as well; a backup on the same
# disk does not survive losing that disk.
set -euo pipefail

BACKUP_DIR=${BACKUP_DIR:-/var/backups/cto-planner}
BACKUP_KEEP_DAYS=${BACKUP_KEEP_DAYS:-14}
COMPOSE=${COMPOSE:-docker compose -f deploy/docker-compose.yml}

umask 077
mkdir -p "$BACKUP_DIR"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
out="$BACKUP_DIR/cto_planner-$stamp.dump"

$COMPOSE exec -T postgres pg_dump -U planner -d cto_planner --format=custom --compress=9 > "$out.partial"
# A dump that pg_restore cannot list is not a backup.
$COMPOSE exec -T postgres pg_restore --list < "$out.partial" > /dev/null
mv "$out.partial" "$out"
sha256sum "$out" > "$out.sha256"
echo "$(date -u +%FT%TZ) backup written: $out ($(du -h "$out" | cut -f1))"

find "$BACKUP_DIR" -name 'cto_planner-*.dump*' -mtime +"$BACKUP_KEEP_DAYS" -print -delete
