package planning

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/arshadm25/cto-delivery-planner/internal/audit"
)

// Team is a planner team made of Redmine users.
type Team struct {
	ID      int64        `json:"id"`
	Name    string       `json:"name"`
	Members []TeamMember `json:"members"`
}

// TeamMember is one Redmine user in a team. HoursPerWeek is their usual
// availability, used to suggest a team's available hours.
type TeamMember struct {
	PersonID     int      `json:"person_id"`
	PersonName   *string  `json:"person_name"`
	HoursPerWeek *float64 `json:"hours_per_week"`
}

// TeamInput creates or replaces a team.
type TeamInput struct {
	Name    string       `json:"name"`
	Members []TeamMember `json:"members"`
}

// Teams returns every team with its members, by name.
func (s *Store) Teams(ctx context.Context) ([]Team, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name FROM planner_teams ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	teams, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Team, error) {
		t := Team{Members: []TeamMember{}}
		err := r.Scan(&t.ID, &t.Name)
		return t, err
	})
	if err != nil || len(teams) == 0 {
		return teams, err
	}
	idx := map[int64]int{}
	for i, t := range teams {
		idx[t.ID] = i
	}
	rows, err = s.Pool.Query(ctx, `SELECT m.team_id, m.person_redmine_id, u.name, m.hours_per_week::float8
		FROM planner_team_members m LEFT JOIN redmine_users u ON u.redmine_id = m.person_redmine_id
		ORDER BY u.name NULLS LAST, m.person_redmine_id`)
	if err != nil {
		return nil, err
	}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (int64, error) {
		var id int64
		var m TeamMember
		if err := r.Scan(&id, &m.PersonID, &m.PersonName, &m.HoursPerWeek); err != nil {
			return 0, err
		}
		if i, ok := idx[id]; ok {
			teams[i].Members = append(teams[i].Members, m)
		}
		return id, nil
	})
	return teams, err
}

func (in *TeamInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return invalid("a team needs a name")
	}
	seen := map[int]bool{}
	for _, m := range in.Members {
		if m.PersonID <= 0 {
			return invalid("unknown team member")
		}
		if seen[m.PersonID] {
			return invalid("a person is listed twice in the team")
		}
		seen[m.PersonID] = true
		if m.HoursPerWeek != nil && (*m.HoursPerWeek < 0 || *m.HoursPerWeek > 168) {
			return invalid("hours per week must be between 0 and 168")
		}
	}
	return nil
}

// SaveTeam creates (id 0) or replaces a team and its members.
func (s *Store) SaveTeam(ctx context.Context, by Actor, id int64, in TeamInput) (int64, error) {
	if err := in.validate(); err != nil {
		return 0, err
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		action := "update"
		if id == 0 {
			action = "create"
			if err := tx.QueryRow(ctx, `INSERT INTO planner_teams (name, created_by) VALUES ($1, $2) RETURNING id`, in.Name, by.ID).Scan(&id); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE planner_teams SET name = $2, updated_at = now() WHERE id = $1`, id, in.Name)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotFound
			}
			if _, err := tx.Exec(ctx, `DELETE FROM planner_team_members WHERE team_id = $1`, id); err != nil {
				return err
			}
		}
		for _, m := range in.Members {
			var known bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM redmine_users WHERE redmine_id = $1)`, m.PersonID).Scan(&known); err != nil {
				return err
			}
			if !known {
				return invalid("person %d is not a Redmine user", m.PersonID)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO planner_team_members (team_id, person_redmine_id, hours_per_week) VALUES ($1, $2, $3)`,
				id, m.PersonID, m.HoursPerWeek); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, by.String(), action, "planner_teams", strconv.FormatInt(id, 10), nil, in)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return 0, invalid("a team with this name already exists")
	}
	return id, err
}

// DeleteTeam removes a team that no capacity allocation uses.
func (s *Store) DeleteTeam(ctx context.Context, by Actor, id int64) error {
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM planner_teams WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Record(ctx, tx, by.String(), "delete", "planner_teams", strconv.FormatInt(id, 10), nil, nil)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return invalid("this team has capacity allocations; delete or change them first")
	}
	return err
}
