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
