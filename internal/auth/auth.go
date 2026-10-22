// Package auth holds dashboard accounts, sessions and the role model.
//
// Accounts are local (Q8 is open; SSO can replace Authenticate later).
// Passwords are bcrypt hashes. Session tokens are random 32-byte values;
// only their SHA-256 is stored, so a database copy cannot be used to log in.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Role is a dashboard role.
type Role string

const (
	Admin          Role = "admin"
	CTO            Role = "cto"
	ProjectManager Role = "project_manager"
	TechLead       Role = "tech_lead"
	Viewer         Role = "viewer"
)

// Roles lists every valid role.
var Roles = []Role{Admin, CTO, ProjectManager, TechLead, Viewer}

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	for _, x := range Roles {
		if r == x {
			return true
		}
	}
	return false
}

// Permission is an action guarded by role.
type Permission string

const (
	ManageUsers        Permission = "manage_users"
	EditSettings       Permission = "edit_settings"
	EditProjectRoles   Permission = "edit_project_roles"
	ProposeCommitments Permission = "propose_commitments"
	ApproveCommitments Permission = "approve_commitments"
	EditCapacity       Permission = "edit_capacity"
	ApproveCapacity    Permission = "approve_capacity"
)

var grants = map[Role][]Permission{
	Admin:          {ManageUsers, EditSettings, EditProjectRoles},
	CTO:            {EditSettings, EditProjectRoles, ProposeCommitments, ApproveCommitments, EditCapacity, ApproveCapacity},
	ProjectManager: {ProposeCommitments, EditCapacity},
	TechLead:       {EditCapacity},
	Viewer:         {},
}

// User is a dashboard account.
type User struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Role        Role       `json:"role"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// SeesAllProjects is true for roles that are not limited to assigned projects.
func (u *User) SeesAllProjects() bool { return u.Role == Admin || u.Role == CTO }

// Can reports whether the user's role grants p.
func (u *User) Can(p Permission) bool {
	for _, g := range grants[u.Role] {
		if g == p {
			return true
		}
	}
	return false
}

// Permissions lists what the user may do, for the UI.
func (u *User) Permissions() []Permission { return append([]Permission{}, grants[u.Role]...) }

var (
	ErrInvalidLogin = errors.New("auth: invalid email or password")
	ErrNoSession    = errors.New("auth: no valid session")
	ErrWeakPassword = errors.New("auth: password must be at least 12 characters")
	ErrDuplicate    = errors.New("auth: an account with this email exists")
)

// MinPasswordLength is enforced on create and reset.
const MinPasswordLength = 12

// Store reads and writes accounts and sessions.
type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
	cost int
}

// NewStore returns a Store using bcrypt.DefaultCost.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: time.Now, cost: bcrypt.DefaultCost}
}

// WithCost lowers the bcrypt cost; only for tests.
func (s *Store) WithCost(c int) *Store { s.cost = c; return s }

// dummyHash makes a login for an unknown email take as long as a real one.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

const userCols = `id, email, display_name, role, disabled, created_at, last_login_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt, &u.LastLoginAt)
	return u, err
}

// CreateUser adds an account.
func (s *Store) CreateUser(ctx context.Context, email, name, password string, role Role) (User, error) {
	email = strings.TrimSpace(email)
	name = strings.TrimSpace(name)
	if !strings.Contains(email, "@") || name == "" {
		return User{}, errors.New("auth: email and display name are required")
	}
	if !role.Valid() {
		return User{}, fmt.Errorf("auth: unknown role %q", role)
	}
	hash, err := s.hash(password)
	if err != nil {
		return User{}, err
	}
	u, err := scanUser(s.pool.QueryRow(ctx, `INSERT INTO app_users (email, display_name, password_hash, role)
		VALUES ($1, $2, $3, $4) RETURNING `+userCols, email, name, hash, role))
	if err != nil && strings.Contains(err.Error(), "app_users_email") {
		return User{}, ErrDuplicate
	}
	return u, err
}

func (s *Store) hash(password string) (string, error) {
	if len(password) < MinPasswordLength {
		return "", ErrWeakPassword
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	return string(h), err
}

// SetPassword replaces a password and ends the user's sessions.
func (s *Store) SetPassword(ctx context.Context, userID int64, password string) error {
	hash, err := s.hash(password)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE app_users SET password_hash = $1 WHERE id = $2`, hash, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM app_sessions WHERE user_id = $1`, userID)
		return err
	})
}

// UpdateUser changes role, name or disabled state. Disabling ends sessions.
func (s *Store) UpdateUser(ctx context.Context, id int64, name *string, role *Role, disabled *bool) (User, error) {
	if role != nil && !role.Valid() {
		return User{}, fmt.Errorf("auth: unknown role %q", *role)
	}
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		u, err = scanUser(tx.QueryRow(ctx, `UPDATE app_users SET
			display_name = COALESCE($2, display_name), role = COALESCE($3, role), disabled = COALESCE($4, disabled)
			WHERE id = $1 RETURNING `+userCols, id, name, role, disabled))
		if err != nil {
			return err
		}
		if u.Disabled || role != nil {
			_, err = tx.Exec(ctx, `DELETE FROM app_sessions WHERE user_id = $1`, id)
		}
		return err
	})
	return u, err
}

// ListUsers returns all accounts.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM app_users ORDER BY lower(email)`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (User, error) { return scanUser(r) })
}

// Authenticate checks an email and password.
func (s *Store) Authenticate(ctx context.Context, email, password string) (User, error) {
	var hash string
	var u User
	err := s.pool.QueryRow(ctx, `SELECT `+userCols+`, password_hash FROM app_users WHERE lower(email) = lower($1)`,
		strings.TrimSpace(email)).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Disabled, &u.CreatedAt, &u.LastLoginAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return User{}, ErrInvalidLogin
	}
	if err != nil {
		return User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || u.Disabled {
		return User{}, ErrInvalidLogin
	}
	s.pool.Exec(ctx, `UPDATE app_users SET last_login_at = $1 WHERE id = $2`, s.now(), u.ID)
	return u, nil
}

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// NewSession creates a session and returns its token.
func (s *Store) NewSession(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	now := s.now()
	_, err := s.pool.Exec(ctx, `INSERT INTO app_sessions (token_hash, user_id, created_at, expires_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $3)`, tokenHash(token), userID, now, now.Add(ttl))
	// Drop expired sessions now and then.
	s.pool.Exec(ctx, `DELETE FROM app_sessions WHERE expires_at < $1`, now)
	return token, err
}

// UserForToken returns the active user for a session token.
func (s *Store) UserForToken(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrNoSession
	}
	u, err := scanUser(s.pool.QueryRow(ctx, `UPDATE app_sessions s SET last_seen_at = $2
		FROM app_users u WHERE s.token_hash = $1 AND s.user_id = u.id AND s.expires_at > $2 AND NOT u.disabled
		RETURNING u.id, u.email, u.display_name, u.role, u.disabled, u.created_at, u.last_login_at`, tokenHash(token), s.now()))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNoSession
	}
	return u, err
}

// EndSession deletes a session.
func (s *Store) EndSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM app_sessions WHERE token_hash = $1`, tokenHash(token))
	return err
}

// Access is the set of projects a user may see.
type Access struct {
	All bool
	IDs map[int]bool
}

// Allows reports whether the project is visible.
func (a Access) Allows(projectID int) bool { return a.All || a.IDs[projectID] }

// ProjectAccess loads the user's project access.
func (s *Store) ProjectAccess(ctx context.Context, u User) (Access, error) {
	if u.SeesAllProjects() {
		return Access{All: true}, nil
	}
	ids, err := s.AssignedProjects(ctx, u.ID)
	if err != nil {
		return Access{}, err
	}
	a := Access{IDs: map[int]bool{}}
	for _, id := range ids {
		a.IDs[id] = true
	}
	return a, nil
}

// AssignedProjects lists the project IDs assigned to a user.
func (s *Store) AssignedProjects(ctx context.Context, userID int64) ([]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT project_redmine_id FROM app_user_projects WHERE user_id = $1 ORDER BY 1`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int])
}

// SetAssignedProjects replaces a user's project list.
func (s *Store) SetAssignedProjects(ctx context.Context, userID int64, ids []int) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM app_user_projects WHERE user_id = $1`, userID); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.Exec(ctx, `INSERT INTO app_user_projects (user_id, project_redmine_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING`, userID, id); err != nil {
				return err
			}
		}
		return nil
	})
}
