package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidCursor = errors.New("invalid cursor")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetUserByID(ctx context.Context, userID uuid.UUID) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, email, status, kyc_level, mfa_enabled, created_at
		FROM users
		WHERE id = $1
	`, userID)

	var user User
	if err := row.Scan(&user.ID, &user.Email, &user.Status, &user.KYCLevel, &user.MFAEnabled, &user.CreatedAt); err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) ListAccounts(ctx context.Context, userID uuid.UUID, limit int, cursor string) ([]Account, string, error) {
	limit = clampLimit(limit)

	query := `
		SELECT id, type, status, created_at
		FROM accounts
		WHERE user_id = $1
	`
	args := []any{userID}
	limitParam := 2

	if cursor != "" {
		ts, id, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		query += " AND (created_at, id) > ($2, $3)"
		args = append(args, ts, id)
		limitParam = 4
	}

	query += fmt.Sprintf(" ORDER BY created_at, id LIMIT $%d", limitParam)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	items := make([]Account, 0, limit)
	var nextCursor string
	for rows.Next() {
		var acc Account
		if err := rows.Scan(&acc.ID, &acc.Type, &acc.Status, &acc.CreatedAt); err != nil {
			return nil, "", err
		}
		items = append(items, acc)
	}

	if len(items) > limit {
		last := items[limit]
		items = items[:limit]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return items, nextCursor, rows.Err()
}

func (s *Store) ListBalances(ctx context.Context, userID uuid.UUID, limit int, cursor string) ([]Balance, string, error) {
	limit = clampLimit(limit)

	query := `
		SELECT la.account_id, la.asset, la.balance_available::text, la.balance_locked::text, la.updated_at
		FROM ledger_accounts la
		JOIN accounts a ON a.id = la.account_id
		WHERE a.user_id = $1
	`
	args := []any{userID}
	limitParam := 2

	if cursor != "" {
		ts, id, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		query += " AND (la.updated_at, la.account_id) > ($2, $3)"
		args = append(args, ts, id)
		limitParam = 4
	}

	query += fmt.Sprintf(" ORDER BY la.updated_at, la.account_id LIMIT $%d", limitParam)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	items := make([]Balance, 0, limit)
	var nextCursor string
	for rows.Next() {
		var bal Balance
		if err := rows.Scan(&bal.AccountID, &bal.Asset, &bal.Available, &bal.Locked, &bal.UpdatedAt); err != nil {
			return nil, "", err
		}
		items = append(items, bal)
	}

	if len(items) > limit {
		last := items[limit]
		items = items[:limit]
		nextCursor = encodeCursor(last.UpdatedAt, last.AccountID)
	}

	return items, nextCursor, rows.Err()
}

func (s *Store) InsertAudit(ctx context.Context, log AuditLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs (actor_id, actor_type, action, entity_type, entity_id, created_at, metadata)
		VALUES ($1, $2, $3, $4, $5, now(), $6)
	`, log.ActorID, log.ActorType, log.Action, log.EntityType, log.EntityID, map[string]string{
		"ip":         log.IP,
		"user_agent": log.UserAgent,
	})
	return err
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func encodeCursor(ts time.Time, id uuid.UUID) string {
	payload := fmt.Sprintf("%s|%s", ts.UTC().Format(time.RFC3339Nano), id.String())
	return base64.StdEncoding.EncodeToString([]byte(payload))
}

func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	return ts, id, nil
}
