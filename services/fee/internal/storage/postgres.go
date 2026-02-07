package storage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetAllFeeTiers(ctx context.Context) ([]FeeTier, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, maker_fee_bps, taker_fee_bps, min_volume::text
		FROM fee_tiers
		ORDER BY min_volume DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tiers []FeeTier
	for rows.Next() {
		var tier FeeTier
		if err := rows.Scan(&tier.ID, &tier.Name, &tier.MakerFeeBps, &tier.TakerFeeBps, &tier.MinVolume); err != nil {
			return nil, err
		}
		tiers = append(tiers, tier)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return tiers, nil
}

func (s *Store) GetFeeTierByVolume(ctx context.Context, volume string) (*FeeTier, error) {
	var tier FeeTier
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, maker_fee_bps, taker_fee_bps, min_volume::text
		FROM fee_tiers
		WHERE min_volume <= $1
		ORDER BY min_volume DESC
		LIMIT 1
	`, volume)

	if err := row.Scan(&tier.ID, &tier.Name, &tier.MakerFeeBps, &tier.TakerFeeBps, &tier.MinVolume); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &tier, nil
}

func (s *Store) GetDefaultFeeTier(ctx context.Context) (*FeeTier, error) {
	var tier FeeTier
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, maker_fee_bps, taker_fee_bps, min_volume::text
		FROM fee_tiers
		WHERE name = 'default'
		LIMIT 1
	`)

	if err := row.Scan(&tier.ID, &tier.Name, &tier.MakerFeeBps, &tier.TakerFeeBps, &tier.MinVolume); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &tier, nil
}

func (s *Store) GetAccountVolume30d(ctx context.Context, accountID uuid.UUID) (string, error) {
	var volume string
	row := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(volume), 0)::text
		FROM (
			SELECT (t.price * t.quantity) AS volume
			FROM trades t
			JOIN orders o ON o.id = t.maker_order_id
			WHERE o.account_id = $1 AND t.executed_at >= now() - interval '30 days'
			UNION ALL
			SELECT (t.price * t.quantity) AS volume
			FROM trades t
			JOIN orders o ON o.id = t.taker_order_id
			WHERE o.account_id = $1 AND t.executed_at >= now() - interval '30 days'
		) v
	`, accountID)

	if err := row.Scan(&volume); err != nil {
		return "", fmt.Errorf("query volume: %w", err)
	}
	return volume, nil
}
