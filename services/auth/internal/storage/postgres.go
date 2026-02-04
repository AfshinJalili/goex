package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, status, kyc_level, mfa_enabled, mfa_secret
		FROM users
		WHERE email = $1
	`, email)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Status, &user.KYCLevel, &user.MFAEnabled, &user.MFASecret); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetRefreshTokenByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
	`, hash)

	var token RefreshToken
	if err := row.Scan(&token.ID, &token.UserID, &token.TokenHash, &token.ExpiresAt, &token.RevokedAt); err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *Store) CreateRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time, ip string, userAgent string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at, created_ip, user_agent)
		VALUES ($1, $2, $3, now(), $4, $5)
		RETURNING id
	`, userID, tokenHash, expiresAt, ip, userAgent).Scan(&id)
	return id, err
}

func (s *Store) RevokeTokenByHash(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, hash)
	return err
}

func (s *Store) RevokeAllTokens(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}

func (s *Store) RotateToken(ctx context.Context, oldTokenID uuid.UUID, userID uuid.UUID, newHash string, expiresAt time.Time, ip string, userAgent string) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var newID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at, created_ip, user_agent)
		VALUES ($1, $2, $3, now(), $4, $5)
		RETURNING id
	`, userID, newHash, expiresAt, ip, userAgent).Scan(&newID); err != nil {
		return uuid.Nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now(), replaced_by = $2
		WHERE id = $1
	`, oldTokenID, newID); err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	return newID, nil
}
