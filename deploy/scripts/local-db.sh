#!/usr/bin/env bash
# Local development database without Docker (P2-01). Creates the `planner`
# role and the `cto_planner` database on a PostgreSQL server you already run,
# such as Homebrew's postgresql@16 on a Mac. Safe to run more than once.
#
#   brew install postgresql@16 && brew services start postgresql@16
#   make local-db
#
# The role gets CREATEDB because `make test-integration` creates and drops
# its own throwaway databases. The password is the same development-only
# value as deploy/compose.dev.yml, so .env.example works unchanged.
set -euo pipefail

PSQL=${PSQL:-psql}
ADMIN_DB=${ADMIN_DB:-postgres}
ROLE=planner
DB=cto_planner

q() { $PSQL -d "$ADMIN_DB" -v ON_ERROR_STOP=1 -qAt -c "$1"; }

if ! q "SELECT 1" >/dev/null 2>&1; then
  echo "Cannot connect to PostgreSQL with '$PSQL -d $ADMIN_DB'. Is the server running?" >&2
  echo "On a Mac: brew services start postgresql@16" >&2
  exit 1
fi

if [ "$(q "SELECT 1 FROM pg_roles WHERE rolname = '$ROLE'")" != "1" ]; then
  q "CREATE ROLE $ROLE LOGIN CREATEDB PASSWORD 'planner-dev-only'"
  echo "created role $ROLE"
else
  q "ALTER ROLE $ROLE LOGIN CREATEDB"
  echo "role $ROLE already exists"
fi

if [ "$(q "SELECT 1 FROM pg_database WHERE datname = '$DB'")" != "1" ]; then
  q "CREATE DATABASE $DB OWNER $ROLE"
  echo "created database $DB"
else
  echo "database $DB already exists"
fi

echo "Ready: postgres://$ROLE:planner-dev-only@localhost:5432/$DB?sslmode=disable"
