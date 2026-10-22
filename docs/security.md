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
- [ ] Response headers checked: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`.
- [ ] Decide on a Content-Security-Policy. None is set yet; add it at the reverse proxy or in `apps/web/next.config.ts`.

## Dependencies

- [ ] `go list -m -u all` and `npm audit` reviewed; no known high-severity issues left unaddressed.

## Findings

| # | Finding | Severity | Decision | Owner | Due |
|---|---|---|---|---|---|
| | | | | | |
