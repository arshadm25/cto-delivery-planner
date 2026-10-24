# Commitments, capacity, forecasts and reports

Backlog items P4-01 to P4-06. P4-07, entering the existing approved baselines, is a task for the planner's users.

## Commitments (P4-01, Q5)

- A commitment covers either a whole project or one Redmine version. It has a baseline date, a title and a required reason.
- **Proposing.** Project managers and the CTO can propose. A proposal for a scope that already has an approved baseline is a *revision* and points at that baseline.
- **Approving.** Only the CTO can approve or reject, and a rejection needs a note. Approving a revision marks the old baseline `superseded`. The old row and its date are kept.
- **Delivered.** "Mark delivered" records the actual date. Baseline, forecast and actual are always three separate fields.
- **Audit.** Every step is written to `audit_events` in the same transaction.
- **One live baseline.** A database constraint allows only one live (approved or delivered) baseline per scope.

Who can approve is set by role. That matches "Approver role: TBD" in the plan: change `grants` in `internal/auth` if someone other than the CTO should approve.

## Capacity (P4-02, Q7)

- **What an allocation records:**
  - a person (a Redmine user ID) or a team name;
  - a period;
  - available hours;
  - free-text assumptions;
  - hours allocated per project.
- **Validation.** Allocations cannot exceed available hours. Project managers can only allocate to projects they can see.
- **Draft and approval.** New allocations are drafts. Only the CTO approves. Editing an approved allocation puts it back to draft.
- **What feeds the numbers.** Only approved allocations feed forecasts and the workload view. Hours are prorated by working days.
- **Working calendar.** Set under Settings: working days, hours per day and holidays. Until someone saves it, every forecast says the calendar is not confirmed.

## Forecast rules (P4-03, plan section 11)

`internal/forecast`, unit-tested. Each forecast returns its method, inputs, assumptions and confidence.

| Situation | Method | Confidence |
|---|---|---|
| At least half of open issues are estimated and approved capacity exists | Remaining estimate × (1 − done ratio), scaled up for unestimated issues, ÷ weekly allocated hours, in working days | high when 80%+ are estimated, otherwise medium |
| Otherwise, 3+ issues closed in the last 8 weeks, in at least 3 different weeks | Open issues ÷ average weekly closures | low; medium when 10+ closures across 6+ weeks |
| Neither | **TBD** | none |

Further rules:

- **External blockers.** If an open issue outside the scope blocks an issue inside it, the forecast is never earlier than that blocker's due date.
- **Forecast after baseline.** Raises a high **forecast after baseline** flag, so the project's health becomes at risk.
- **Capacity risk.** Raised when remaining effort exceeds approved capacity before the baseline.
- **No approved baseline.** Gives a low data-gap flag, and the forecast status stays TBD.

## Planning in the planner

Decided on 2026-10-24: planning happens in the planner, not in Redmine. Nothing is written back to Redmine, and the Redmine key stays read-only.

- **Issue values.** On the project page, the CTO, project managers and tech leads can set an issue's estimate, due date, assignee and milestone. They can do it for one issue (Edit) or for many at once (tick the issues, then "Set … to …").
- **Planner values win.** A value set in the planner replaces Redmine's in every view, flag, forecast, snapshot and the workload page, and it survives every sync. The issue list marks it "planner" and shows Redmine's value on hover. Clearing the value, or "use Redmine", goes back to Redmine's.
- **Milestones.** Milestones can be created in the planner next to Redmine's versions. Inside the app they are versions with a negative ID. A commitment can target them like any version. Deleting one sends its issues back to their Redmine version. Deletion is refused while a commitment points at it.
- **Teams.** Teams are built on the Capacity page from Redmine users, with each member's usual hours per week. Capacity can then be allocated to a team, and the team's usual hours suggest its available hours for the period.
- **Audit.** Every change is written to `audit_events`.
- **Storage.** Values are in `issue_plans`, `planner_milestones`, `planner_teams` and `planner_team_members` (migration 0004).

## Parents, milestone order, scope and QA (2026-10-24)

- **Parent projects.** A project with in-scope subprojects is planned with them: its whole-project forecast covers its own issues and every subproject's, with their capacity added up, and its whole-project baseline is the baseline for all of them. Subprojects keep their own forecasts too. A viewer who cannot see every subproject gets the parent's own forecast instead, so hidden work never leaks through it. The weekly report uses the combined forecast.
- **Milestones one after another.** Milestone baselines in a project are forecast in baseline-date order. Each milestone starts after the open work of the approved ones before it, since the same people deliver them. Before this, every milestone assumed the project's whole capacity.
- **Scope frozen at approval.** Approving a baseline stores the issues in its scope and their estimates (`commitment_scope`). The Delivery card then shows issues added and removed since, and how estimates moved. Baselines approved before this change have no frozen scope.
- **QA days.** Settings → Rule settings → "Working days of QA after development". When it is set, the forecast adds those working days after development finishes, shows both dates, and is a release date. When only QA is left, the forecast is today plus the QA days, with low confidence. 0 (the default) keeps the old behaviour.

## Planner tasks

Work that is not in Redmine can be added as a task on the project's Issues tab. A task has a subject and takes an estimate, due date, assignee and milestone like any issue. It counts in forecasts, flags, workload and scope. It can be marked done, reopened or deleted. Tasks are stored in `planner_tasks` and are shown as issues with negative IDs. Redmine issues still open and close only in Redmine.

## Awaiting QA

Some open Redmine statuses mean development is finished and the work is waiting for testing. At discovery (P1-12), 67% of open in-scope issues were in *Fixed Not Tested*. The CTO decided on 2026-10-22 that these issues count separately.

- They stay open and are not delivered until Redmine closes them. Milestones still count them as open work.
- They are left out of remaining development effort, estimate coverage, forecasts and workload.
- They do not get stale, missing-estimate or over-estimate flags. They still get overdue, blocked, missing-owner and missing-date flags.
- Each project shows how many issues are awaiting QA, and the issue list can be filtered to them.

The statuses are set under **Settings → Rule settings**. The default is *Fixed Not Tested*, and an empty list turns the bucket off.

## Snapshots and history (P4-04, Q12)

- **When.** The API takes one snapshot per project per day, and an admin or the CTO can take one on demand (`POST /api/v1/snapshots`).
- **What each snapshot stores:**
  - health and its reasons;
  - baseline, forecast, confidence and method;
  - issue counts;
  - remaining estimate and estimate coverage;
  - flag counts.
- **Retention.** Controlled by `SNAPSHOT_RETENTION_DAYS`, default 730. That default is an assumption until Q12 is answered.
- **History.** The project page shows the last 90 days.

## Workload (P4-05)

- **Per person:**
  - open issues;
  - remaining estimate;
  - unestimated count;
  - estimate due inside the period;
  - available and allocated hours;
  - hours logged in the last 28 days, when time entries were imported.
- **Unassigned work** is listed separately for each project.
- **No ranking.** Rows are sorted by name, and the page says it is not for comparing people.
- **Over capacity.** A person is flagged when the work due in the period exceeds their available hours, or when their allocations exceed availability.

## Weekly CTO report (P4-06)

`/reports/weekly` compares today with the snapshot from a week ago, or with the latest earlier one if there is none from a week ago. It covers:

- what changed, with causes;
- forecast changes;
- milestones due in 14 days;
- blockers and forecast risks;
- capacity conflicts;
- decisions needed: proposals and draft allocations waiting for approval, late forecasts, projects without a baseline;
- data-quality asks for project managers.

The Markdown download (`?format=md`) shows people as `User #id` so it can be shared. `&names=full` turns the masking off.
