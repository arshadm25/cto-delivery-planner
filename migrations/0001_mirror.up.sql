-- Redmine mirror tables (plan section 8). Source IDs are the primary keys,
-- so a re-import cannot create duplicates. Source fields that Redmine may
-- leave out are nullable; nothing defaults to zero.

CREATE TABLE redmine_projects (
    redmine_id        integer PRIMARY KEY,
    name              text NOT NULL,
    identifier        text NOT NULL,
    status            integer NOT NULL,
    is_public         boolean,
    parent_redmine_id integer,
    created_on        timestamptz,
    updated_on        timestamptz,
    in_scope          boolean NOT NULL DEFAULT false,
    last_synced_at    timestamptz NOT NULL,
    -- Set when the project stops appearing in the API. Rows are never deleted.
    inactive_since    timestamptz
);

-- Only the ID and display name Redmine puts on issues. No login, email or
-- other personal details.
CREATE TABLE redmine_users (
    redmine_id     integer PRIMARY KEY,
    name           text NOT NULL,
    last_synced_at timestamptz NOT NULL
);

CREATE TABLE redmine_statuses (
    redmine_id     integer PRIMARY KEY,
    name           text NOT NULL,
    is_closed      boolean NOT NULL,
    last_synced_at timestamptz NOT NULL
);

CREATE TABLE redmine_versions (
    redmine_id         integer PRIMARY KEY,
    project_redmine_id integer NOT NULL,
    name               text NOT NULL,
    status             text NOT NULL,
    sharing            text,
    effective_date     date,
    created_on         timestamptz,
    updated_on         timestamptz,
    last_synced_at     timestamptz NOT NULL,
    inactive_since     timestamptz
);

CREATE TABLE redmine_issues (
    redmine_id               integer PRIMARY KEY,
    project_redmine_id       integer NOT NULL,
    parent_redmine_id        integer,
    subject                  text NOT NULL,
    tracker_id               integer NOT NULL,
    tracker_name             text NOT NULL,
    status_id                integer NOT NULL,
    status_name              text NOT NULL,
    priority_id              integer NOT NULL,
    priority_name            text NOT NULL,
    author_redmine_id        integer,
    assigned_to_redmine_id   integer,
    fixed_version_redmine_id integer,
    start_date               date,
    due_date                 date,
    done_ratio               integer,
    estimated_hours          numeric(10,2),
    spent_hours              numeric(10,2),
    is_private               boolean,
    created_on               timestamptz NOT NULL,
    updated_on               timestamptz NOT NULL,
    closed_on                timestamptz,
    -- Hash of the mirrored fields, used to count unchanged rows on re-import.
    source_hash              text NOT NULL,
    first_synced_at          timestamptz NOT NULL,
    last_synced_at           timestamptz NOT NULL,
    -- Set by reconciliation when the issue is no longer returned (deleted,
    -- moved out of scope or hidden from the service account).
    inactive_since           timestamptz
);
CREATE INDEX redmine_issues_project ON redmine_issues (project_redmine_id) WHERE inactive_since IS NULL;
CREATE INDEX redmine_issues_assignee ON redmine_issues (assigned_to_redmine_id) WHERE inactive_since IS NULL;
CREATE INDEX redmine_issues_version ON redmine_issues (fixed_version_redmine_id);
CREATE INDEX redmine_issues_updated ON redmine_issues (updated_on);

CREATE TABLE redmine_issue_relations (
    redmine_id            integer PRIMARY KEY,
    issue_from_redmine_id integer NOT NULL,
    issue_to_redmine_id   integer NOT NULL,
    relation_type         text NOT NULL,
    delay                 integer,
    last_synced_at        timestamptz NOT NULL
);
CREATE INDEX redmine_issue_relations_from ON redmine_issue_relations (issue_from_redmine_id);
CREATE INDEX redmine_issue_relations_to ON redmine_issue_relations (issue_to_redmine_id);

-- Comments are not copied.
CREATE TABLE redmine_time_entries (
    redmine_id         integer PRIMARY KEY,
    project_redmine_id integer NOT NULL,
    issue_redmine_id   integer,
    user_redmine_id    integer NOT NULL,
    activity           text NOT NULL,
    hours              numeric(10,2) NOT NULL,
    spent_on           date NOT NULL,
    updated_on         timestamptz,
    last_synced_at     timestamptz NOT NULL,
    inactive_since     timestamptz
);
CREATE INDEX redmine_time_entries_issue ON redmine_time_entries (issue_redmine_id);
CREATE INDEX redmine_time_entries_user_day ON redmine_time_entries (user_redmine_id, spent_on);

CREATE TABLE sync_runs (
    id            bigserial PRIMARY KEY,
    kind          text NOT NULL CHECK (kind IN ('full', 'incremental')),
    started_at    timestamptz NOT NULL,
    heartbeat_at  timestamptz NOT NULL,
    finished_at   timestamptz,
    outcome       text NOT NULL CHECK (outcome IN ('running', 'succeeded', 'partial', 'failed', 'abandoned')),
    -- {"issues": {"fetched": 0, "created": 0, "updated": 0, "unchanged": 0, "inactivated": 0}, ...}
    counts        jsonb NOT NULL DEFAULT '{}',
    checkpoint_before timestamptz,
    checkpoint_after  timestamptz,
    warnings      jsonb NOT NULL DEFAULT '[]',
    error_summary text
);
CREATE INDEX sync_runs_started ON sync_runs (started_at DESC);

CREATE TABLE sync_checkpoints (
    name       text PRIMARY KEY,
    value      timestamptz NOT NULL,
    run_id     bigint NOT NULL REFERENCES sync_runs (id),
    updated_at timestamptz NOT NULL
);

-- Per-project result of comparing stored issues with the API total_count.
CREATE TABLE sync_reconciliation (
    run_id        bigint NOT NULL REFERENCES sync_runs (id) ON DELETE CASCADE,
    project_redmine_id integer NOT NULL,
    api_total     integer,
    stored_active integer NOT NULL,
    matches       boolean NOT NULL,
    note          text,
    PRIMARY KEY (run_id, project_redmine_id)
);

CREATE TABLE audit_events (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL DEFAULT now(),
    actor      text NOT NULL,
    action     text NOT NULL,
    entity     text NOT NULL,
    entity_id  text,
    before     jsonb,
    after      jsonb
);
CREATE INDEX audit_events_entity ON audit_events (entity, entity_id, at DESC);
