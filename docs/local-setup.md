# Local setup without Docker

Backlog item P2-01. `make up` starts PostgreSQL in Docker. If you don't have Docker, run PostgreSQL 16 directly and use `make local-db` instead. Everything else is the same.

## Mac (Homebrew)

```sh
brew install go node postgresql@16
brew services start postgresql@16
make local-db             # creates role `planner` and database `cto_planner`; safe to re-run
cp .env.example .env      # DATABASE_URL already points at this database
make migrate              # create the tables
```

`make local-db` connects as your own macOS user, which Homebrew makes a PostgreSQL superuser. On another setup, point it at an admin account, for example `PSQL="psql -U postgres -h localhost" make local-db`.

## Try the dashboard with fictional data

```sh
APP_ENV=development make devseed            # fake Redmine data and demo accounts
make api                                    # Go API on :8080
cd apps/web && npm install && npm run dev   # dashboard on http://localhost:3000
```

Demo accounts are listed in [dashboard.md](dashboard.md).

## Import your real Redmine data

Fill in `REDMINE_URL` and `REDMINE_API_KEY` in `.env` on your own machine, then follow [sync.md](sync.md) from `make sync-full`. The key never leaves `.env`.

## Tests

```sh
make test               # unit tests; database tests skip
make test-integration   # also runs the database tests against the local server
make web-e2e            # Playwright; needs E2E_DATABASE_URL pointing at a throwaway database
```

For `make web-e2e`, create a throwaway database first, for example `createdb -O planner planner_e2e`, and set `E2E_DATABASE_URL=postgres://planner:planner-dev-only@localhost:5432/planner_e2e?sslmode=disable`.

## Stop or reset

```sh
brew services stop postgresql@16
dropdb cto_planner && make local-db && make migrate   # start over with an empty database
```
