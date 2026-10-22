#!/usr/bin/env bash
# Alert when sync fails or data goes stale (P5-05). Run from cron every
# 15 minutes. On failure it prints the problem, posts it to
# ALERT_WEBHOOK_URL if set (any endpoint accepting {"text": "..."} JSON,
# such as a Slack or Teams incoming webhook; channel TBD), and exits 1.
#
#   */15 * * * * cd /opt/cto-delivery-planner && ALERT_WEBHOOK_URL=... deploy/scripts/check-health.sh >> /var/log/cto-planner-health.log 2>&1
set -uo pipefail

COMPOSE=${COMPOSE:-docker compose -f deploy/docker-compose.yml}
problems=()

check() {
  local name=$1 svc=$2 url=$3 body
  if ! body=$($COMPOSE exec -T "$svc" /app/probe "$url" 2>&1); then
    problems+=("$name: ${body:-no response}")
  fi
}

check "api ready" api http://localhost:8080/readyz
check "worker ready" worker http://localhost:8081/readyz
check "sync health" api http://localhost:8080/health/sync

if [ ${#problems[@]} -eq 0 ]; then
  echo "$(date -u +%FT%TZ) ok"
  exit 0
fi

msg="CTO Delivery Planner on $(hostname): ${problems[*]}"
echo "$(date -u +%FT%TZ) $msg" >&2
if [ -n "${ALERT_WEBHOOK_URL:-}" ]; then
  payload=$(printf '%s' "$msg" | python3 -c 'import json,sys; print(json.dumps({"text": sys.stdin.read()}))')
  curl -fsS -m 10 -H 'Content-Type: application/json' -d "$payload" "$ALERT_WEBHOOK_URL" > /dev/null || echo "alert post failed" >&2
fi
exit 1
