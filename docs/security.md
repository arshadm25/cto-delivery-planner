# Security review checklist (P5-06)

Reviewer: TBD. Date: TBD. Tick each item after checking it on the deployed host, not just in the code. Record findings and decisions at the bottom.

## Read-only against Redmine

- [ ] `internal/redmine` client rejects any method other than GET (unit-tested).
- [ ] The service account has view-only permissions in Redmine and is not an admin.
- [ ] Only the `worker` container is on the `egress` network.

## Secrets

- [ ] `/etc/cto-planner/planner.env` is mode 600, owned by root, outside the repo.
- [ ] No secrets in the repo, images or Compose files (`.dockerignore` excludes `.env*`; `git log -p | grep -i api_key` shows only placeholders).
- [ ] The Redmine key is never sent to the browser (the web app only talks to `/api/v1`).
- [ ] Log redaction: values of `REDMINE_API_KEY` and `DATABASE_URL`, and any attribute named like password, secret, token, api key, authorization or cookie, are replaced with `[REDACTED]`. Verify with `docker compose logs | grep -c <first 6 chars of key>` returning 0.
- [ ] Key rotation procedure tested once ([operations.md](operations.md) section 4).

## Authentication and sessions

- [ ] Passwords are bcrypt-hashed, minimum 12 characters.
- [ ] Session tokens are random and stored only as SHA-256 hashes.
- [ ] Cookie is HttpOnly, SameSite=Strict and Secure; served only over HTTPS.
- [ ] Session lifetime (`SESSION_TTL`) agreed.
- [ ] Login is rate-limited to 10 failures per IP or email per 15 minutes.
- [ ] Unknown emails take the same time as wrong passwords.
- [ ] Decide whether SSO is needed later (Q8; local accounts for now).

## Access control

- [ ] Every API route except `/healthz`, `/readyz`, `/health/sync` and login requires a session.
- [ ] Project managers, tech leads and viewers see only assigned projects; other projects answer 404 (tested in `internal/api`).
- [ ] Only CTO approves commitments and capacity; confirm this matches the agreed approver role.
- [ ] Every write needs the `X-Requested-With: planner` header (CSRF guard).
- [ ] Admin user list reviewed; leavers disabled.

## Personal data and retention

- [ ] The mirror stores Redmine user ID and display name only (no emails, no logins).
- [ ] Weekly Markdown export masks names by default.
- [ ] Snapshot retention (`SNAPSHOT_RETENTION_DAYS`, default 730) agreed (Q12).
- [ ] Backup retention (`BACKUP_KEEP_DAYS`) and off-host copy location agreed and access-controlled.
- [ ] Audit log covers settings, roles, commitments, capacity and user changes.

## Network and host

- [ ] PostgreSQL has no published port; API is not published.
- [ ] Web is published on 127.0.0.1 only, behind a TLS reverse proxy.
- [ ] Reverse proxy sets HSTS and forwards only to the web port.
- [ ] Containers run non-root, read-only, all capabilities dropped (`docker inspect`).
- [ ] Host OS and Docker patched; base images rebuilt on upgrade.
- [ ] Response headers checked: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, and `Content-Security-Policy` (set in `apps/web/next.config.ts` for pages, and `default-src 'none'` on every API response).
- [ ] The reverse proxy does not replace or drop the app's `Content-Security-Policy` header.

## Dependencies

- [ ] `go list -m -u all` and `npm audit` reviewed; no known high-severity issues left unaddressed.

## Code-side review, 2026-11-09

Done by Claude against `main` without access to the deployed host, so the checkboxes above stay open until someone checks them on the host. Confirmed in the code and tests:

- Read-only Redmine client: every non-GET method is refused (`TestNonGetRequestsAreRefused`).
- Passwords: bcrypt, minimum 12 characters (`internal/auth`). Sessions: random tokens stored as SHA-256 hashes; cookie HttpOnly, SameSite=Strict, Secure unless `COOKIE_SECURE=false`.
- Login rate limit (`TestLoginRateLimit`), CSRF header guard on every write (`internal/api/server.go`), 1 MB request body limit.
- Project-level access enforced in the API (`TestProjectManagerSeesOnlyAssignedProjects`, `TestViewerWithoutProjectsGetsNothing`).
- Secret redaction in logs (`TestLoggerRedacts`); no `NEXT_PUBLIC_*` variable except the API base path.
- Mirror stores user ID and display name only; no issue descriptions or time-entry comments are copied ([sync.md](sync.md)).
- Content-Security-Policy added in this review; the end-to-end suite checks it is sent and causes no violations.
- `npm audit` and `go vet` run; results in the findings below. `govulncheck` could not reach its database from the review environment, so Go dependencies still need a check.

## Findings

| # | Finding | Severity | Decision | Owner | Due |
|---|---|---|---|---|---|
| 1 | No Content-Security-Policy on pages or API | Medium | Fixed 2026-11-09: CSP in `next.config.ts` and on API responses | Claude | Done |
| 2 | `@playwright/test` 1.55.0 downloads browsers without verifying TLS (GHSA-7mvr-c777-76hp). Test tooling only, not shipped | Low | Fixed 2026-11-09: upgraded to 1.55.1 | Claude | Done |
| 3 | `next` 15.5.x bundles `postcss` 8.4.31 with advisories (GHSA-qx2v-qp2m-jg93 and related). It only processes the app's own CSS at build time, so no untrusted input reaches it | Low | Open: the fix is the Next 16 major upgrade. Decide whether to schedule it | Repo owner | TBD |
| 4 | CSP allows `'unsafe-inline'` scripts because Next.js inlines bootstrap scripts | Low | Accepted for now: no user-supplied HTML is rendered; a nonce-based CSP would need middleware and dynamic rendering | Repo owner | TBD |
| 5 | Go dependencies not checked with `govulncheck` | Low | Open: run `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` on a machine with internet access | Repo owner | TBD |
| 6 | Only the CTO role approves commitments and capacity, which was assumed, not agreed | Low | Open: confirm the approver role | Repo owner | TBD |
| 7 | `APP_TIMEZONE` defaults to UTC, so dates near midnight can land on the wrong day for a team in India | Low | Open: set `APP_TIMEZONE=Asia/Kolkata` on the host if that is the team's zone | Repo owner | TBD |
