package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"telcopulse/services/shared/domain"
)

var (
	ErrForbidden = errors.New("administrator role required")
	ErrConflict  = errors.New("user management conflict")
	ErrNotFound  = errors.New("user not found")
)

var roles = []string{"Viewer", "Operator", "Engineer", "Incident Commander", "Administrator"}
var userIDPattern = regexp.MustCompile(`^USR-[a-f0-9]{24}$`)

type ManagedUser struct {
	User
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type UsersPage struct {
	Items []ManagedUser `json:"items"`
	More  bool          `json:"more"`
	Next  string        `json:"next,omitempty"`
}

type CreateUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type UpdateUser struct {
	Role   *string `json:"role,omitempty"`
	Active *bool   `json:"active,omitempty"`
}

type userCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func validPassword(password string) bool {
	return len(password) >= 16 && len(password) <= 72 && strings.TrimSpace(password) == password
}

func (s Store) adminInTx(ctx context.Context, tx pgx.Tx, token string) (string, error) {
	if !tokenPattern.MatchString(token) {
		return "", ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	var id, role string
	err := tx.QueryRow(ctx, `SELECT u.id,u.role FROM auth.sessions s JOIN auth.users u ON u.id=s.user_id
 WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND u.active=true FOR UPDATE OF u`, digest[:]).Scan(&id, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	if role != "Administrator" {
		return "", ErrForbidden
	}
	return id, nil
}

func (s Store) ListUsers(ctx context.Context, token, cursor string) (UsersPage, error) {
	out := UsersPage{Items: []ManagedUser{}}
	var boundary userCursor
	if len(cursor) > 512 {
		return out, ErrInvalid
	}
	if cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(data, &boundary) != nil || boundary.At.IsZero() || !userIDPattern.MatchString(boundary.ID) {
			return out, ErrInvalid
		}
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = s.adminInTx(ctx, tx, token); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,username,role,active,created_at FROM auth.users
 WHERE ($1='' OR (created_at,id)<($2,$1)) ORDER BY created_at DESC,id DESC LIMIT 51`, boundary.ID, boundary.At)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var user ManagedUser
		if err = rows.Scan(&user.ID, &user.Username, &user.Role, &user.Active, &user.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, user)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Items) > 50 {
		out.More = true
		out.Items = out.Items[:50]
		last := out.Items[49]
		data, err := json.Marshal(userCursor{At: last.CreatedAt, ID: last.ID})
		if err != nil {
			return out, err
		}
		out.Next = base64.RawURLEncoding.EncodeToString(data)
	}
	return out, tx.Commit(ctx)
}

func (s Store) CreateUser(ctx context.Context, token string, input CreateUser) (ManagedUser, error) {
	var out ManagedUser
	if !usernamePattern.MatchString(input.Username) || !validPassword(input.Password) || !slices.Contains(roles, input.Role) {
		return out, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actor, err := s.adminInTx(ctx, tx, token)
	if err != nil {
		return out, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		return out, err
	}
	id, err := domain.NewID(12)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO auth.users(id,username,password_hash,role) VALUES($1,$2,$3,$4)
 RETURNING id,username,role,active,created_at`, "USR-"+id, input.Username, string(hash), input.Role).Scan(&out.ID, &out.Username, &out.Role, &out.Active, &out.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ManagedUser{}, ErrConflict
		}
		return ManagedUser{}, err
	}
	if err = insertManagementEvent(ctx, tx, out.ID, input.Username, actor, "user_created", map[string]any{"role": out.Role}); err != nil {
		return ManagedUser{}, err
	}
	return out, tx.Commit(ctx)
}

func (s Store) UpdateUser(ctx context.Context, token, id string, input UpdateUser) (ManagedUser, error) {
	var out ManagedUser
	if !userIDPattern.MatchString(id) || (input.Role == nil && input.Active == nil) || (input.Role != nil && !slices.Contains(roles, *input.Role)) {
		return out, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('auth-admin-management',24))"); err != nil {
		return out, err
	}
	actor, err := s.adminInTx(ctx, tx, token)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, "SELECT id,username,role,active,created_at FROM auth.users WHERE id=$1 FOR UPDATE", id).Scan(&out.ID, &out.Username, &out.Role, &out.Active, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ManagedUser{}, ErrNotFound
	}
	if err != nil {
		return ManagedUser{}, err
	}
	previous := out
	if input.Role != nil {
		out.Role = *input.Role
	}
	if input.Active != nil {
		out.Active = *input.Active
	}
	if previous.Role == "Administrator" && previous.Active && (out.Role != "Administrator" || !out.Active) {
		var count int
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM auth.users WHERE role='Administrator' AND active=true").Scan(&count); err != nil {
			return ManagedUser{}, err
		}
		if count <= 1 {
			return ManagedUser{}, ErrConflict
		}
	}
	if previous.Role == out.Role && previous.Active == out.Active {
		return out, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "UPDATE auth.users SET role=$2,active=$3 WHERE id=$1", id, out.Role, out.Active); err != nil {
		return ManagedUser{}, err
	}
	if previous.Role != out.Role {
		if err = insertManagementEvent(ctx, tx, id, out.Username, actor, "role_changed", map[string]any{"before": previous.Role, "after": out.Role}); err != nil {
			return ManagedUser{}, err
		}
	}
	if previous.Active != out.Active {
		action := "user_deactivated"
		if out.Active {
			action = "user_reactivated"
		} else if _, err = tx.Exec(ctx, "UPDATE auth.sessions SET revoked_at=clock_timestamp() WHERE user_id=$1 AND revoked_at IS NULL", id); err != nil {
			return ManagedUser{}, err
		}
		if err = insertManagementEvent(ctx, tx, id, out.Username, actor, action, map[string]any{"before": previous.Active, "after": out.Active}); err != nil {
			return ManagedUser{}, err
		}
	}
	return out, tx.Commit(ctx)
}

func insertManagementEvent(ctx context.Context, tx pgx.Tx, subjectID, username, actorID, action string, details map[string]any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth.events(user_id,username_digest,action,actor_user_id,details)
 VALUES($1,$2,$3,$4,$5::jsonb)`, subjectID, digestUsername(username), action, actorID, string(data))
	return err
}
