# CTO Delivery Planner

A portfolio delivery dashboard for the CTO and delivery team, connected to Redmine through its REST API.

Redmine stays the source of truth. The planner only reads from Redmine: it never creates, updates or deletes Redmine data. The Redmine client refuses any request that is not a GET.

## Status

Phase 1, discovery and baseline validation. What exists today:

| Path | What it is |
|---|---|
| `cmd/redmine-discovery` | Read-only CLI that builds the Phase 1 baseline report |
| `internal/redmine` | Read-only Redmine REST client: pagination, retries, timeouts |
| `internal/discovery` | Counts, project hierarchy and data-gap checks behind the report |
| `docs/discovery.md` | How to run discovery on a trusted host |
| `docs/redmine-environment.md` | Facts about the Redmine install, filled in during Phase 1 |

The Next.js dashboard (`apps/web`), Go API and sync worker (`services/`) and PostgreSQL migrations come in later phases.

## Rules

- The Redmine API key lives only on the trusted host, in `.env` or a secret manager. It is never committed, pasted into chat, or exposed to browser code.
- Missing estimates, dates and owners stay unknown. Nothing is defaulted to zero.
- Reports that leave the host identify people by Redmine user ID only.

## Development

Requires Go 1.24 or later.

```bash
make test        # unit tests
make vet
make fmt-check
```
