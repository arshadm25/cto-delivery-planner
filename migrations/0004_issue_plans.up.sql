-- Planning done in the planner (decided 2026-10-24). Nothing here is
-- written back to Redmine: these values replace Redmine's in every view,
-- forecast and flag, and Redmine's own values stay in redmine_issues.

-- Milestones created in the planner. The API shows them as versions with a
-- negative ID (-id), so they never collide with Redmine version IDs.
CREATE TABLE planner_milestones (
    id                 bigserial PRIMARY KEY,
    project_redmine_id integer NOT NULL,
    name               text NOT NULL CHECK (btrim(name) <> ''),
    due_date           date,
    status             text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    created_by         bigint NOT NULL REFERENCES app_users (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX planner_milestones_project ON planner_milestones (project_redmine_id);

-- One row per issue that has planner values. A NULL column means "use the
-- Redmine value". version_id is a Redmine version ID, or -id of a planner
-- milestone.
CREATE TABLE issue_plans (
    issue_redmine_id    integer PRIMARY KEY,
    estimated_hours     numeric(8,2) CHECK (estimated_hours >= 0),
    due_date            date,
    assignee_redmine_id integer,
    version_id          integer,
    updated_by          bigint NOT NULL REFERENCES app_users (id),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX issue_plans_version ON issue_plans (version_id) WHERE version_id IS NOT NULL;

-- Teams built in the planner from Redmine users. hours_per_week is the
-- member's usual availability, used to suggest a team's available hours.
CREATE TABLE planner_teams (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL CHECK (btrim(name) <> ''),
    created_by bigint NOT NULL REFERENCES app_users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX planner_teams_name ON planner_teams (lower(name));

CREATE TABLE planner_team_members (
    team_id           bigint NOT NULL REFERENCES planner_teams (id) ON DELETE CASCADE,
    person_redmine_id integer NOT NULL,
    hours_per_week    numeric(6,2) CHECK (hours_per_week >= 0),
    PRIMARY KEY (team_id, person_redmine_id)
);

-- An allocation is now for a person, a planner team, or (older rows) a
-- typed team name.
ALTER TABLE capacity_allocations ADD COLUMN team_id bigint REFERENCES planner_teams (id);
DO $$
DECLARE c text;
BEGIN
    SELECT conname INTO c FROM pg_constraint
    WHERE conrelid = 'capacity_allocations'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%team_name%';
    EXECUTE format('ALTER TABLE capacity_allocations DROP CONSTRAINT %I', c);
END $$;
ALTER TABLE capacity_allocations ADD CONSTRAINT capacity_allocations_one_owner CHECK (
    (person_redmine_id IS NOT NULL)::int + (team_name IS NOT NULL)::int + (team_id IS NOT NULL)::int = 1);
