-- Planner-owned delivery commitments, capacity and snapshots (plan section 8).

-- A baseline is never edited in place. A revision is a new row that
-- supersedes the approved one once it is itself approved.
CREATE TABLE delivery_commitments (
    id                 bigserial PRIMARY KEY,
    project_redmine_id integer NOT NULL,
    -- NULL means the whole project; otherwise one Redmine version.
    version_redmine_id integer,
    title              text NOT NULL,
    baseline_date      date NOT NULL,
    status             text NOT NULL CHECK (status IN ('proposed', 'approved', 'rejected', 'superseded', 'delivered')),
    actual_date        date,
    reason             text NOT NULL,
    proposed_by        bigint NOT NULL REFERENCES app_users (id),
    proposed_at        timestamptz NOT NULL DEFAULT now(),
    decided_by         bigint REFERENCES app_users (id),
    decided_at         timestamptz,
    decision_note      text,
    supersedes_id      bigint REFERENCES delivery_commitments (id)
);
-- At most one live baseline per project or version.
CREATE UNIQUE INDEX delivery_commitments_one_approved
    ON delivery_commitments (project_redmine_id, COALESCE(version_redmine_id, 0))
    WHERE status IN ('approved', 'delivered');

-- Available hours for a person in a period, and how they are split across
-- projects.
CREATE TABLE capacity_allocations (
    id                 bigserial PRIMARY KEY,
    person_redmine_id  integer,
    team_name          text,
    period_start       date NOT NULL,
    period_end         date NOT NULL,
    available_hours    numeric(8,2),
    assumptions        text NOT NULL DEFAULT '',
    status             text NOT NULL CHECK (status IN ('draft', 'approved')),
    created_by         bigint NOT NULL REFERENCES app_users (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    approved_by        bigint REFERENCES app_users (id),
    approved_at        timestamptz,
    CHECK (period_end >= period_start),
    CHECK ((person_redmine_id IS NULL) <> (team_name IS NULL))
);
CREATE INDEX capacity_allocations_period ON capacity_allocations (period_start, period_end);

CREATE TABLE capacity_allocation_projects (
    allocation_id      bigint NOT NULL REFERENCES capacity_allocations (id) ON DELETE CASCADE,
    project_redmine_id integer NOT NULL,
    allocated_hours    numeric(8,2) NOT NULL CHECK (allocated_hours >= 0),
    PRIMARY KEY (allocation_id, project_redmine_id)
);

-- One row per project per day. Retention is set by SNAPSHOT_RETENTION_DAYS (Q12).
CREATE TABLE project_snapshots (
    snapshot_date            date NOT NULL,
    project_redmine_id       integer NOT NULL,
    taken_at                 timestamptz NOT NULL,
    health                   text NOT NULL,
    health_reasons           jsonb NOT NULL,
    baseline_date            date,
    forecast_date            date,
    forecast_confidence      text NOT NULL,
    forecast_method          text NOT NULL,
    open_issues              integer NOT NULL,
    closed_issues            integer NOT NULL,
    overdue_issues           integer NOT NULL,
    blocked_issues           integer NOT NULL,
    remaining_estimated_hours numeric(10,2),
    estimate_coverage        numeric(5,4),
    flag_counts              jsonb NOT NULL,
    PRIMARY KEY (snapshot_date, project_redmine_id)
);
