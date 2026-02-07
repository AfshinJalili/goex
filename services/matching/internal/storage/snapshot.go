package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/services/matching/internal/engine"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type SnapshotStore struct {
	pool *pgxpool.Pool
}

func NewSnapshotStore(pool *pgxpool.Pool) *SnapshotStore {
	return &SnapshotStore{pool: pool}
}

func (s *SnapshotStore) LoadOpenOrders(ctx context.Context, symbol string) ([]*engine.Order, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("db pool not configured")
	}

	args := []any{}
	query := `
		SELECT id::text, client_order_id, account_id::text, symbol, side, type,
		       price::text, quantity::text, filled_quantity::text, time_in_force, created_at
		FROM orders
		WHERE status IN ('open', 'partially_filled')
	`
	if strings.TrimSpace(symbol) != "" {
		query += " AND symbol = $1"
		args = append(args, symbol)
	}
	query += " ORDER BY created_at ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []*engine.Order{}
	for rows.Next() {
		var id, clientID, accountID, sym, side, orderType, qtyStr, filledStr, tif string
		var priceStr *string
		var createdAt time.Time
		if err := rows.Scan(&id, &clientID, &accountID, &sym, &side, &orderType, &priceStr, &qtyStr, &filledStr, &tif, &createdAt); err != nil {
			return nil, err
		}
		qty, err := decimal.NewFromString(strings.TrimSpace(qtyStr))
		if err != nil {
			return nil, fmt.Errorf("parse quantity: %w", err)
		}
		filled := decimal.Zero
		if strings.TrimSpace(filledStr) != "" {
			filled, err = decimal.NewFromString(strings.TrimSpace(filledStr))
			if err != nil {
				return nil, fmt.Errorf("parse filled quantity: %w", err)
			}
		}
		price := decimal.Zero
		if priceStr != nil && strings.TrimSpace(*priceStr) != "" {
			price, err = decimal.NewFromString(strings.TrimSpace(*priceStr))
			if err != nil {
				return nil, fmt.Errorf("parse price: %w", err)
			}
		}

		orders = append(orders, &engine.Order{
			ID:            id,
			ClientOrderID: clientID,
			AccountID:     accountID,
			Symbol:        sym,
			Side:          side,
			Type:          orderType,
			Price:         price,
			Quantity:      qty,
			Filled:        filled,
			TimeInForce:   tif,
			CreatedAt:     createdAt,
		})
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return orders, nil
}
