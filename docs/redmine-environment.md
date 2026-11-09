# Redmine environment

Facts about the Redmine installation, filled in during Phase 1 (backlog item P1-02). Leave a field as TBD until it has been checked.

| Item | Value | Where it comes from |
|---|---|---|
| Base URL | Recorded in the host's env file, kept out of this public repo | User, 2026-10-23 |
| Redmine version | 5.1.12.stable | Administration › Information (user, 2026-10-23) |
| Ruby / Rails version | TBD | Administration › Information |
| Installed plugins | TBD | Administration › Plugins |
| REST API enabled | Yes | Administration › Settings › API (user, 2026-10-23) |
| Reverse proxy or rate limits in front of Redmine | TBD | Server config |
| Service account login | Created 2026-10-23; login kept out of this public repo | Administration › Users |
| Service account is non-admin | Yes | Phase 1 exit review P1-13, accepted by the repo owner 2026-10-24 (see [scope.md](scope.md)) |
| Projects the service account is a member of | 44 visible, covering all 38 in-scope projects in [`config/scope.json`](../config/scope.json) | Non-admin discovery run, P1-13, 2026-10-24 |
| Maximum issues page size the server returns | TBD | `limit` in the discovery responses |
