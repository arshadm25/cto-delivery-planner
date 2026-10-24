# Dashboard and API

Backlog items P3-01 to P3-09.

## Run it locally with fictional data

```sh
make up                                   # Postgres
APP_ENV=development make devseed          # fake Redmine data and demo accounts
make api                                  # Go API on :8080 (COOKIE_SECURE=false for local http)
cd apps/web && npm install && npm run dev # dashboard on http://localhost:3000
```

Demo accounts are `admin@`, `cto@`, `pm@` and `viewer@example.test` with password `demo-password-123`. `devseed` refuses to run unless `APP_ENV=development`.

On a real host, create the first administrator with:

```sh
PLANNER_NEW_PASSWORD='…' ./bin/api -create-user you@company.example -name "Your Name" -role admin
```

## Login and roles (Q4, Q8)

- Local accounts with bcrypt passwords (12 characters minimum) and server-side sessions. Only a SHA-256 of the session token is stored. The cookie is `HttpOnly`, `SameSite=Strict` and `Secure` unless `COOKIE_SECURE=false`.
- Every write must send `X-Requested-With: planner`, which a cross-site form cannot set.
- Ten failed logins per IP or per email in 15 minutes lock that key for the rest of the window.
- SSO is not decided (Q8). `auth.Store.Authenticate` is the single place to replace.

| Role | Sees | Can change |
|---|---|---|
| Administrator | All in-scope projects | Users and project access, settings, project owner and tech lead |
| CTO | All in-scope projects | Settings, project owner and tech lead, commitments (approve), capacity (approve), plan issues and milestones |
| Project manager | Assigned projects | Propose commitments, capacity and teams, plan issues and milestones |
| Tech lead | Assigned projects | Capacity and teams, plan issues and milestones |
| Viewer | Assigned projects | Nothing |

A project the user may not see answers 404 on every endpoint, so its existence is not disclosed. Project owner and tech lead are planner-owned fields that show TBD until someone records them (Q4 is still open).

## API

All under `/api/v1`, JSON, cookie session. The Next.js app forwards `/api/v1/*` to `API_INTERNAL_URL` at build time, so the browser only talks to its own origin.

| Method | Path | Purpose |
|---|---|---|
| POST | `/auth/login`, `/auth/logout` | Session |
| GET | `/auth/me` | Current user, permissions, assigned projects |
| GET | `/portfolio/summary` | Overview KPIs, health counts, upcoming milestones, last sync |
| GET | `/projects?health=` | Project rows with health and counts |
| GET | `/projects/{id}` | Project detail, blockers, data gaps |
| GET | `/projects/{id}/issues?status=&flag=&assignee=&version=&tracker=&q=&sort=&page=&per_page=` | Issues with flags and Redmine links |
| GET | `/projects/{id}/milestones` | Versions with progress and flags |
| PUT | `/projects/{id}/roles` | Owner and tech lead |
| POST | `/projects/{id}/plan` | Set estimate, due date, assignee or milestone on one or more issues (`{"issue_ids": [...], "changes": {...}}`; `null` goes back to Redmine) |
| POST, PUT, DELETE | `/projects/{id}/planner-milestones[/{mid}]` | Milestones created in the planner (`mid` is minus the version ID) |
| GET, POST, PUT, DELETE | `/teams[/{id}]` | Teams built from Redmine users |
| GET | `/risks?project=&kind=&severity=&page=` | All flags with evidence |
| GET | `/sync/status` | Recent runs, last success, checkpoint, reconciliation |
| GET, PUT | `/settings/rules` | Thresholds |
| GET, POST, PATCH, PUT | `/admin/users…` | Accounts and project access |

## Flags and health

Rules live in `internal/risks` and are unit-tested. Every flag has evidence, a suggested action and a link to the Redmine issue or version.

| Flag | Severity | Rule |
|---|---|---|
| Blocked dependency | critical if the issue's priority is in the high list, else high | An open issue blocks or precedes it |
| Overdue | high | Open and due date before today |
| Milestone overdue | high | Open version past its date with open issues |
| Milestone at risk | high | Due within the warning window with more than half its issues open |
| Release readiness | high | Version named with a release keyword (QA, UAT, Release, Handover) due within the window with open issues |
| Stale activity | medium | No update for `stale_after_days` (default 14, Q12) |
| No assignee | medium | Open with no assignee |
| No estimate, no date, over estimate, milestone without date | low | Data-quality gaps |

Health: **blocked** if any critical flag, **at risk** if any high flag, **not enough data** if nothing is open or no open issue or milestone has a date, otherwise **on track**. Medium and low flags never change health; they show as data gaps.

"Today" uses `APP_TIMEZONE` (default UTC). The thresholds are starting values on the Settings page, not agreed policy.

## Tests

- `internal/api/api_test.go` runs the API against PostgreSQL seeded through the real sync worker and the fake Redmine. It covers the CTO overview, project-manager scoping, a viewer with no projects, 401/403/404 paths, the CSRF header, login rate limiting, a visible sync failure that keeps previous data, honest unknowns and re-import without duplicates.
- `apps/web/e2e` drives the built dashboard in Chromium against the API: login redirect, wrong password, CTO overview, project-manager scoping, TBD owners, Redmine links, sync status and admin pages hidden from a viewer.
