package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrInvalidCursor       = errors.New("invalid cursor")
	ErrInvalidStatus       = errors.New("invalid status transition")
	ErrAlreadyHandled      = errors.New("already processed")
	orderIngestEventPrefix = "order-ingest:"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetAccountIDForUser(ctx context.Context, userID uuid.UUID) (uuid.UUID, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id
		FROM accounts
		WHERE user_id = $1 AND status = 'active'
		ORDER BY created_at ASC
		LIMIT 1
	`, userID)
	var accountID uuid.UUID
	if err := row.Scan(&accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return accountID, nil
}

func (s *Store) CreateOrder(ctx context.Context, order Order) (*Order, bool, error) {
	var price string
	priceNull := true
	if order.Price != nil {
		price = order.Price.String()
		priceNull = false
	}

	var row pgx.Row
	if order.ID != uuid.Nil {
		row = s.pool.QueryRow(ctx, `
			INSERT INTO orders (id, client_order_id, account_id, symbol, side, type, price, quantity, filled_quantity, status, time_in_force)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (client_order_id, account_id) DO NOTHING
			RETURNING id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
		`, order.ID, order.ClientOrderID, order.AccountID, order.Symbol, order.Side, order.Type, nullableString(price, priceNull), order.Quantity.String(), order.FilledQuantity.String(), order.Status, order.TimeInForce)
	} else {
		row = s.pool.QueryRow(ctx, `
			INSERT INTO orders (client_order_id, account_id, symbol, side, type, price, quantity, filled_quantity, status, time_in_force)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (client_order_id, account_id) DO NOTHING
			RETURNING id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
		`, order.ClientOrderID, order.AccountID, order.Symbol, order.Side, order.Type, nullableString(price, priceNull), order.Quantity.String(), order.FilledQuantity.String(), order.Status, order.TimeInForce)
	}

	stored, err := scanOrderRow(row)
	if err == nil {
		return stored, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}

	existing, err := s.GetOrderByClientID(ctx, order.AccountID, order.ClientOrderID)
	if err != nil {
		return nil, false, err
	}
	return existing, false, nil
}

func (s *Store) GetOrderByID(ctx context.Context, orderID uuid.UUID) (*Order, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
		FROM orders
		WHERE id = $1
	`, orderID)
	order, err := scanOrderRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return order, nil
}

func (s *Store) GetOrderByClientID(ctx context.Context, accountID uuid.UUID, clientOrderID string) (*Order, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
		FROM orders
		WHERE account_id = $1 AND client_order_id = $2
	`, accountID, clientOrderID)
	order, err := scanOrderRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return order, nil
}

func (s *Store) GetLastTradePrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return decimal.Zero, ErrNotFound
	}
	row := s.pool.QueryRow(ctx, `
		SELECT price::text
		FROM trades
		WHERE symbol = $1
		ORDER BY executed_at DESC
		LIMIT 1
	`, symbol)
	var priceStr string
	if err := row.Scan(&priceStr); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, ErrNotFound
		}
		return decimal.Zero, err
	}
	price, err := decimal.NewFromString(strings.TrimSpace(priceStr))
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse trade price: %w", err)
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, ErrNotFound
	}
	return price, nil
}

func (s *Store) ListOrders(ctx context.Context, accountID uuid.UUID, filter OrderFilter) ([]Order, string, error) {
	limit := clampLimit(filter.Limit)

	query := `
		SELECT id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
		FROM orders
		WHERE account_id = $1
	`
	args := []any{accountID}
	idx := 2

	if filter.Symbol != "" {
		query += fmt.Sprintf(" AND symbol = $%d", idx)
		args = append(args, filter.Symbol)
		idx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, filter.Status)
		idx++
	}
	if filter.From != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, *filter.From)
		idx++
	}
	if filter.To != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", idx)
		args = append(args, *filter.To)
		idx++
	}
	if filter.Cursor != "" {
		ts, id, err := decodeCursor(filter.Cursor)
		if err != nil {
			return nil, "", ErrInvalidCursor
		}
		query += fmt.Sprintf(" AND (created_at, id) > ($%d, $%d)", idx, idx+1)
		args = append(args, ts, id)
		idx += 2
	}

	query += fmt.Sprintf(" ORDER BY created_at, id LIMIT $%d", idx)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	orders := make([]Order, 0, limit)
	for rows.Next() {
		order, err := scanOrderRow(rows)
		if err != nil {
			return nil, "", err
		}
		orders = append(orders, *order)
	}
	if rows.Err() != nil {
		return nil, "", rows.Err()
	}

	var nextCursor string
	if len(orders) > limit {
		last := orders[limit]
		orders = orders[:limit]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return orders, nextCursor, nil
}

func (s *Store) UpdateOrderStatus(ctx context.Context, orderID uuid.UUID, status string, filledQuantity decimal.Decimal) (*Order, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE orders
		SET status = $1, filled_quantity = $2, updated_at = now()
		WHERE id = $3 AND status NOT IN ('cancelled','rejected','expired')
		RETURNING id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
	`, status, filledQuantity.String(), orderID)

	order, err := scanOrderRow(row)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		var existingStatus string
		check := s.pool.QueryRow(ctx, `
			SELECT status
			FROM orders
			WHERE id = $1
		`, orderID)
		if scanErr := check.Scan(&existingStatus); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, scanErr
		}
		return nil, ErrInvalidStatus
	}
	return order, nil
}

func (s *Store) CancelOrder(ctx context.Context, orderID, accountID uuid.UUID) (*Order, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE orders
		SET status = $1, updated_at = now()
		WHERE id = $2 AND account_id = $3 AND status IN ('pending','open')
		RETURNING id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
	`, OrderStatusCancelled, orderID, accountID)

	order, err := scanOrderRow(row)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		var status string
		check := s.pool.QueryRow(ctx, `
			SELECT status
			FROM orders
			WHERE id = $1 AND account_id = $2
		`, orderID, accountID)
		if scanErr := check.Scan(&status); scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, scanErr
		}
		return nil, ErrInvalidStatus
	}
	return order, nil
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

type ApplyTradeResult struct {
	AlreadyProcessed bool
	FilledOrderIDs   []uuid.UUID
}

func (s *Store) ApplyTradeExecution(ctx context.Context, eventID string, fills []OrderFill) (ApplyTradeResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ApplyTradeResult{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if strings.TrimSpace(eventID) != "" {
		processed, err := isEventProcessed(ctx, tx, eventID)
		if err != nil {
			return ApplyTradeResult{}, err
		}
		if processed {
			return ApplyTradeResult{AlreadyProcessed: true}, nil
		}
	}

	now := time.Now().UTC()
	filledOrders := make([]uuid.UUID, 0, len(fills))
	for _, fill := range fills {
		order, err := getOrderForUpdate(ctx, tx, fill.OrderID)
		if err != nil {
			return ApplyTradeResult{}, err
		}

		if order.Status == OrderStatusCancelled || order.Status == OrderStatusRejected || order.Status == OrderStatusExpired {
			return ApplyTradeResult{}, fmt.Errorf("order %s not updatable", order.ID.String())
		}

		newFilled := order.FilledQuantity.Add(fill.Quantity)
		if newFilled.GreaterThan(order.Quantity) {
			newFilled = order.Quantity
		}

		newStatus := OrderStatusOpen
		if newFilled.Equal(order.Quantity) {
			newStatus = OrderStatusFilled
		}

		_, err = tx.Exec(ctx, `
			UPDATE orders
			SET filled_quantity = $1, status = $2, updated_at = $3
			WHERE id = $4
		`, newFilled.String(), newStatus, now, order.ID)
		if err != nil {
			return ApplyTradeResult{}, err
		}
		if newStatus == OrderStatusFilled && order.Status != OrderStatusFilled {
			filledOrders = append(filledOrders, order.ID)
		}
	}

	if strings.TrimSpace(eventID) != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO processed_events (event_id)
			VALUES ($1)
			ON CONFLICT (event_id) DO NOTHING
		`, orderIngestEventKey(eventID)); err != nil {
			return ApplyTradeResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return ApplyTradeResult{}, err
	}

	return ApplyTradeResult{FilledOrderIDs: filledOrders}, nil
}

type OrderFill struct {
	OrderID  uuid.UUID
	Quantity decimal.Decimal
}

func isEventProcessed(ctx context.Context, tx pgx.Tx, eventID string) (bool, error) {
	eventID = orderIngestEventKey(eventID)
	if eventID == "" {
		return false, nil
	}
	var exists bool
	row := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM processed_events WHERE event_id = $1)`, eventID)
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func orderIngestEventKey(eventID string) string {
	trimmed := strings.TrimSpace(eventID)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, orderIngestEventPrefix) {
		return trimmed
	}
	return orderIngestEventPrefix + trimmed
}

func getOrderForUpdate(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (*Order, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, client_order_id, account_id, symbol, side, type, price::text, quantity::text, filled_quantity::text, status, time_in_force, created_at, updated_at
		FROM orders
		WHERE id = $1
		FOR UPDATE
	`, orderID)
	order, err := scanOrderRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return order, nil
}

func scanOrderRow(row pgx.Row) (*Order, error) {
	var order Order
	var priceStr *string
	var qtyStr string
	var filledStr string
	if err := row.Scan(&order.ID, &order.ClientOrderID, &order.AccountID, &order.Symbol, &order.Side, &order.Type, &priceStr, &qtyStr, &filledStr, &order.Status, &order.TimeInForce, &order.CreatedAt, &order.UpdatedAt); err != nil {
		return nil, err
	}

	if priceStr != nil && *priceStr != "" {
		val, err := decimal.NewFromString(*priceStr)
		if err != nil {
			return nil, fmt.Errorf("parse price: %w", err)
		}
		order.Price = &val
	}

	qty, err := decimal.NewFromString(qtyStr)
	if err != nil {
		return nil, fmt.Errorf("parse quantity: %w", err)
	}
	filled, err := decimal.NewFromString(filledStr)
	if err != nil {
		return nil, fmt.Errorf("parse filled quantity: %w", err)
	}
	order.Quantity = qty
	order.FilledQuantity = filled

	return &order, nil
}

func nullableString(value string, null bool) any {
	if null {
		return nil
	}
	return value
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
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
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return ts, id, nil
}
