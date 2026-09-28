// Package auth owns local operator identities and revocable sessions.
package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"telcopulse/services/shared/domain"
)

var (
	ErrUnauthorized = errors.New("invalid credentials or session")
	ErrInvalid      = errors.New("invalid authentication request")
	usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,59}$`)
	tokenPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// User is the current identity and role; client-supplied role claims are ignored.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// Session returns an opaque token only at login, never from validation.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      User      `json:"user"`
}

// Store uses PostgreSQL as the authority for identities and session revocation.
type Store struct {
	Pool      *pgxpool.Pool
	DummyHash []byte
}

// DummyPasswordHash is compared for unknown users to avoid a fast username probe.
func DummyPasswordHash() ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte("synthetic-unknown-operator-password"), 12)
}

// BootstrapLocal creates exactly one administrator in a new local database.
// An existing user registry is never rewritten by a repeated command.
func BootstrapLocal(ctx context.Context, pool *pgxpool.Pool, username, password string) (bool, error) {
	if !usernamePattern.MatchString(username) {
		return false, ErrInvalid
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('auth-bootstrap',23))"); err != nil {
		return false, err
	}
	var existing bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM auth.users)").Scan(&existing); err != nil {
		return false, err
	}
	if existing {
		return false, tx.Commit(ctx)
	}
	password = strings.TrimSuffix(password, "\n")
	if len(password) < 16 || len(password) > 72 || strings.TrimSpace(password) != password {
		return false, ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return false, err
	}
	token, err := domain.NewID(12)
	if err != nil {
		return false, err
	}
	id := "USR-" + token
	if _, err = tx.Exec(ctx, "INSERT INTO auth.users(id,username,password_hash,role) VALUES($1,$2,$3,'Administrator')", id, username, string(hash)); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO auth.events(user_id,username_digest,action) VALUES($1,$2,'bootstrap_admin')", id, digestUsername(username)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// Login applies bounded account lockout and issues a random, hashed-at-rest session.
func (s Store) Login(ctx context.Context, username, password string) (Session, error) {
	var out Session
	if !usernamePattern.MatchString(username) || len(password) < 1 || len(password) > 128 || len(s.DummyHash) == 0 {
		return out, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var hash string
	var failed int
	var lockedUntil *time.Time
	err = tx.QueryRow(ctx, "SELECT id,username,role,password_hash,failed_attempts,locked_until FROM auth.users WHERE username=$1 AND active=true FOR UPDATE", username).Scan(&out.User.ID, &out.User.Username, &out.User.Role, &hash, &failed, &lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(s.DummyHash, []byte(password))
		if _, err = tx.Exec(ctx, "INSERT INTO auth.events(username_digest,action) VALUES($1,'login_failed')", digestUsername(username)); err != nil {
			return Session{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Session{}, err
		}
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		if _, err = tx.Exec(ctx, "INSERT INTO auth.events(user_id,username_digest,action) VALUES($1,$2,'login_locked')", out.User.ID, digestUsername(username)); err != nil {
			return Session{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Session{}, err
		}
		return Session{}, ErrUnauthorized
	}
	if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return Session{}, err
		}
		if lockedUntil != nil {
			failed = 0
		}
		failed++
		if _, err = tx.Exec(ctx, `UPDATE auth.users SET failed_attempts=$2,locked_until=CASE WHEN $2>=5 THEN clock_timestamp()+interval '15 minutes' ELSE NULL END WHERE id=$1`, out.User.ID, failed); err != nil {
			return Session{}, err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO auth.events(user_id,username_digest,action) VALUES($1,$2,'login_failed')", out.User.ID, digestUsername(username)); err != nil {
			return Session{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Session{}, err
		}
		return Session{}, ErrUnauthorized
	}
	if _, err = tx.Exec(ctx, "UPDATE auth.users SET failed_attempts=0,locked_until=NULL WHERE id=$1", out.User.ID); err != nil {
		return Session{}, err
	}
	out.Token, err = domain.NewID(32)
	if err != nil {
		return Session{}, err
	}
	digest := sha256.Sum256([]byte(out.Token))
	if err = tx.QueryRow(ctx, "INSERT INTO auth.sessions(token_hash,user_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '8 hours') RETURNING expires_at", digest[:], out.User.ID).Scan(&out.ExpiresAt); err != nil {
		return Session{}, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO auth.events(user_id,username_digest,action) VALUES($1,$2,'login_succeeded')", out.User.ID, digestUsername(username)); err != nil {
		return Session{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return out, nil
}

// Validate reads the user's current role and active state on every check.
func (s Store) Validate(ctx context.Context, token string) (User, error) {
	var out User
	if !tokenPattern.MatchString(token) {
		return out, ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	err := s.Pool.QueryRow(ctx, `SELECT u.id,u.username,u.role FROM auth.sessions s JOIN auth.users u ON u.id=s.user_id
 WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND u.active=true`, digest[:]).Scan(&out.ID, &out.Username, &out.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthorized
	}
	return out, err
}

// Logout revokes one session and never returns a reusable credential.
func (s Store) Logout(ctx context.Context, token string) error {
	if !tokenPattern.MatchString(token) {
		return ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID, username string
	err = tx.QueryRow(ctx, `UPDATE auth.sessions s SET revoked_at=clock_timestamp() FROM auth.users u
 WHERE s.token_hash=$1 AND s.revoked_at IS NULL AND u.id=s.user_id RETURNING u.id,u.username`, digest[:]).Scan(&userID, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO auth.events(user_id,username_digest,action) VALUES($1,$2,'logout')", userID, digestUsername(username)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func digestUsername(username string) []byte {
	hash := sha256.Sum256([]byte(username))
	return hash[:]
}
