package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
}

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id::text, email",
		email, passwordHash,
	).Scan(&user.ID, &user.Email)
	return user, err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx,
		"SELECT id::text, email, password_hash FROM users WHERE lower(email) = $1",
		email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash)
	return user, err
}

func (s *Store) PasswordHashByID(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id = $1", userID).Scan(&hash)
	return hash, err
}

func (s *Store) CreateSession(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at < now()"); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	_, err := s.pool.Exec(ctx,
		"INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)",
		tokenHash, userID, expiresAt,
	)
	return err
}

func (s *Store) UserIDForSession(ctx context.Context, tokenHash []byte) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx,
		"SELECT user_id::text FROM sessions WHERE token_hash = $1 AND expires_at > now()",
		tokenHash,
	).Scan(&userID)
	return userID, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
	return err
}

// ChangePassword updates only the authenticated user. The old hash condition
// prevents a concurrent password change from being silently overwritten.
func (s *Store) ChangePassword(ctx context.Context, userID, oldHash, newHash string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.Background())

	tag, err := tx.Exec(ctx,
		"UPDATE users SET password_hash = $1 WHERE id = $2 AND password_hash = $3",
		newHash, userID, oldHash,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}