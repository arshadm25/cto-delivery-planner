-- The work a commitment covered when it was approved, so scope added or
-- removed since the baseline can be shown. Issue values are the planner's.
CREATE TABLE commitment_scope (
    commitment_id    bigint NOT NULL REFERENCES delivery_commitments (id) ON DELETE CASCADE,
    issue_redmine_id integer NOT NULL,
    estimated_hours  numeric(8,2),
    was_open         boolean NOT NULL,
    PRIMARY KEY (commitment_id, issue_redmine_id)
);

-- Tasks created in the planner for work that is not in Redmine. The API
-- shows them as issues with a negative ID (-id), so they never collide with
-- Redmine issue IDs, and they take planner values (issue_plans) like any
-- issue.
CREATE TABLE planner_tasks (
    id                 bigserial PRIMARY KEY,
    project_redmine_id integer NOT NULL,
    subject            text NOT NULL CHECK (btrim(subject) <> ''),
    closed_on          timestamptz,
    created_by         bigint NOT NULL REFERENCES app_users (id),
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX planner_tasks_project ON planner_tasks (project_redmine_id);
