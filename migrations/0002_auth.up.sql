-- Dashboard accounts (Q8: local accounts until SSO is decided), sessions,
-- project-level access and planner-owned project roles.

CREATE TABLE app_users (
    id            bigserial PRIMARY KEY,
    email         text NOT NULL,
    display_name  text NOT NULL,
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('admin', 'cto', 'project_manager', 'tech_lead', 'viewer')),
    disabled      boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);
CREATE UNIQUE INDEX app_users_email ON app_users (lower(email));

-- Only a SHA-256 of the session token is stored.
CREATE TABLE app_sessions (
    token_hash   bytea PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES app_users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL,
    expires_at   timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL
);
CREATE INDEX app_sessions_user ON app_sessions (user_id);

-- Projects a project manager, tech lead or viewer may see. Admin and CTO
-- see every in-scope project.
CREATE TABLE app_user_projects (
    user_id            bigint NOT NULL REFERENCES app_users (id) ON DELETE CASCADE,
    project_redmine_id integer NOT NULL,
    PRIMARY KEY (user_id, project_redmine_id)
);

-- Owner and tech lead per project (Q4). Shown as TBD until someone records
-- and verifies them.
CREATE TABLE project_roles (
    project_redmine_id integer PRIMARY KEY,
    owner_name         text,
    tech_lead_name     text,
    verified_by        bigint REFERENCES app_users (id),
    verified_at        timestamptz,
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- Tunable rules, for example the stale-activity threshold (Q12).
CREATE TABLE planner_settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_by bigint REFERENCES app_users (id),
    updated_at timestamptz NOT NULL DEFAULT now()
);
