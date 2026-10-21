# Portfolio scope

Backlog item P1-11. The planner's first release covers the Redmine projects listed by ID in [`config/scope.json`](../config/scope.json).

| | |
|---|---|
| Decided | 2026-10-21, by the repo owner |
| Rule | All client and product delivery projects |
| Excluded | Internal admin projects: DevOps, HR and finance, meetings, project management activities, training, e-HR, and the live-issue log |
| Size at decision time | 39 of 47 visible projects, 9,014 of 9,360 issues (first discovery run, administrator key) |

Only IDs are stored here. Project names are client names and stay out of this public repository; `discovery-out/projects.csv` maps IDs to names on the trusted host.

Each subproject is listed explicitly, so `include_subprojects` is `false`. A new Redmine project is not covered until its ID is added to this file.

## Open points

- Archived projects are not returned by the API, so they could not be considered. Project ID 1 is absent from discovery and is probably archived.
- The counts above came from an administrator key. Discovery must be re-run with the non-admin service account before the baseline is accepted (P1-13), to confirm it sees all 39 projects and their private issues.
- Changing scope later only means editing this file; no schema change is needed.
