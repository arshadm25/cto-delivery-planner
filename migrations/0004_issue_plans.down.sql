DELETE FROM capacity_allocations WHERE team_id IS NOT NULL;
ALTER TABLE capacity_allocations DROP CONSTRAINT capacity_allocations_one_owner;
ALTER TABLE capacity_allocations DROP COLUMN team_id;
ALTER TABLE capacity_allocations ADD CHECK ((person_redmine_id IS NULL) <> (team_name IS NULL));
DROP TABLE planner_team_members;
DROP TABLE planner_teams;
DROP TABLE issue_plans;
DROP TABLE planner_milestones;
