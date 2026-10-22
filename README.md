# CTO Delivery Planner

A portfolio delivery dashboard for the CTO and delivery team, connected to Redmine through its REST API.

Redmine stays the source of truth. The planner only reads from Redmine: it never creates, updates or deletes Redmine data. The Redmine client refuses any request that is not a GET.

## What's here

| Path | What it is |
|---|---|
| `cmd/redmine-discovery`, `cmd/csv-compare` | Phase 1 discovery and CSV comparison tools ([docs/discovery.md](docs/discovery.md)) |
| `cmd/sync-worker`, `internal/syncer` | Full and incremental Redmine import into PostgreSQL ([docs/sync.md](docs/sync.md)) |
| `cmd/api`, `internal/api` | REST API with login, roles and project-level access ([docs/dashboard.md](docs/dashboard.md)) |
| `apps/web` | Next.js dashboard |
| `internal/risks`, `internal/forecast` | Flag, health and forecast rules ([docs/forecasting.md](docs/forecasting.md)) |
| `internal/redmine` | Read-only Redmine client |
| `migrations` | PostgreSQL schema |
| `deploy` | Docker Compose deployment, reverse-proxy examples, backup, restore and alert scripts ([docs/operations.md](docs/operations.md)) |
| `config/scope.json` | In-scope Redmine project IDs ([docs/scope.md](docs/scope.md)) |
| `cmd/devseed`, `internal/redminetest` | Fictional demo data and the fake Redmine used by tests |

## Rules

- The Redmine API key lives only on the trusted host, in `.env` or a secret manager. It is never committed, pasted into chat, or exposed to browser code.
- Missing estimates, dates and owners stay unknown. Nothing is defaulted to zero.
- Reports that leave the host identify people by Redmine user ID only.

## Development

Requires Go 1.24+, Node 22 and Docker.

```bash
make up                 # local PostgreSQL
make test-integration   # Go unit and database tests
APP_ENV=development make devseed && make api   # API with fictional data
cd apps/web && npm install && npm run dev       # dashboard on :3000
```

See [docs/operations.md](docs/operations.md) to deploy, and [docs/security.md](docs/security.md) for the security review checklist.
