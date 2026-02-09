package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	feepb "github.com/AfshinJalili/goex/services/fee/proto/fee/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	referenceTypeTrade = "trade"
	feeAccountEmail    = "fee@system.local"
	feeAccountType     = "fee"
	ledgerEventPrefix  = "ledger:"
)

var (
	ErrOrderNotFound       = errors.New("order not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrReservationNotFound = errors.New("reservation not found")
	ErrReservationClosed   = errors.New("reservation closed")
)

type FeeClient interface {
	CalculateFees(ctx context.Context, in *feepb.CalculateFeesRequest, opts ...grpc.CallOption) (*feepb.CalculateFeesResponse, error)
}

type FeeMetrics interface {
	ObserveFeeCall(status string, duration time.Duration)
	IncFeeRetry()
	IncFeeFailure(reason string)
	IncFeeFallback(policy string)
}

type Store struct {
	pool       *pgxpool.Pool
	feeClient  FeeClient
	feeMetrics FeeMetrics
	logger     *slog.Logger
	feeCache   *feeRateCache
}

func New(pool *pgxpool.Pool, feeClient FeeClient, logger *slog.Logger, feeMetrics FeeMetrics) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		pool:       pool,
		feeClient:  feeClient,
		feeMetrics: feeMetrics,
		logger:     logger,
		feeCache:   newFeeRateCache(15 * time.Minute),
	}
}

func (s *Store) GetBalance(ctx context.Context, accountID uuid.UUID, asset string) (LedgerAccount, error) {
	var acct LedgerAccount
	var availableStr, lockedStr string
	row := s.pool.QueryRow(ctx, `
		SELECT id, account_id, asset, balance_available::text, balance_locked::text, updated_at
		FROM ledger_accounts
		WHERE account_id = $1 AND asset = $2
	`, accountID, asset)

	if err := row.Scan(&acct.ID, &acct.AccountID, &acct.Asset, &availableStr, &lockedStr, &acct.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LedgerAccount{
				AccountID:        accountID,
				Asset:            asset,
				BalanceAvailable: decimal.Zero,
				BalanceLocked:    decimal.Zero,
			}, nil
		}
		return LedgerAccount{}, err
	}

	var err error
	acct.BalanceAvailable, err = decimal.NewFromString(availableStr)
	if err != nil {
		return LedgerAccount{}, fmt.Errorf("parse available balance: %w", err)
	}
	acct.BalanceLocked, err = decimal.NewFromString(lockedStr)
	if err != nil {
		return LedgerAccount{}, fmt.Errorf("parse locked balance: %w", err)
	}
	return acct, nil
}

func (s *Store) ReserveBalance(ctx context.Context, accountID, orderID uuid.UUID, asset string, amount decimal.Decimal) (*BalanceReservation, error) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return nil, fmt.Errorf("asset is required")
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("amount must be positive")
	}
	if orderID == uuid.Nil {
		return nil, fmt.Errorf("order_id is required")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, orderID.String()); err != nil {
		return nil, err
	}

	existing, err := s.getReservationForUpdate(ctx, tx, orderID)
	if err == nil {
		if existing.Status != "active" {
			return nil, ErrReservationClosed
		}
		if existing.AccountID != accountID || strings.ToUpper(existing.Asset) != asset {
			return nil, fmt.Errorf("reservation mismatch")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		committed = true
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	acct, err := s.getOrCreateLedgerAccountForUpdate(ctx, tx, accountID, asset)
	if err != nil {
		return nil, err
	}
	if acct.BalanceAvailable.LessThan(amount) {
		return nil, ErrInsufficientBalance
	}
	acct.BalanceAvailable = acct.BalanceAvailable.Sub(amount)
	acct.BalanceLocked = acct.BalanceLocked.Add(amount)
	now := time.Now().UTC()
	acct.UpdatedAt = now
	if _, err := tx.Exec(ctx, `
		UPDATE ledger_accounts
		SET balance_available = $1, balance_locked = $2, updated_at = $3
		WHERE id = $4
	`, acct.BalanceAvailable.String(), acct.BalanceLocked.String(), now, acct.ID); err != nil {
		return nil, err
	}

	reservationID := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO balance_reservations (id, order_id, account_id, asset, amount, consumed_amount, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 0, 'active', $6, $6)
	`, reservationID, orderID, accountID, asset, amount.String(), now)
	if err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			committed = true
			return s.getReservation(ctx, orderID)
		} else {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	if existing != nil {
		return existing, nil
	}

	return &BalanceReservation{
		ID:             reservationID,
		OrderID:        orderID,
		AccountID:      accountID,
		Asset:          asset,
		Amount:         amount,
		ConsumedAmount: decimal.Zero,
		Status:         "active",
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (s *Store) ReleaseReservation(ctx context.Context, orderID uuid.UUID) (*BalanceReservation, decimal.Decimal, error) {
	if orderID == uuid.Nil {
		return nil, decimal.Zero, fmt.Errorf("order_id is required")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, decimal.Zero, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	reservation, err := s.getReservationForUpdate(ctx, tx, orderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, decimal.Zero, ErrReservationNotFound
		}
		return nil, decimal.Zero, err
	}

	remaining := reservation.Amount.Sub(reservation.ConsumedAmount)
	if remaining.LessThanOrEqual(decimal.Zero) {
		if reservation.Status != "consumed" {
			if err := s.updateReservation(ctx, tx, reservation, reservation.ConsumedAmount, "consumed"); err != nil {
				return nil, decimal.Zero, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, decimal.Zero, err
		}
		committed = true
		return reservation, decimal.Zero, nil
	}

	acct, err := s.getOrCreateLedgerAccountForUpdate(ctx, tx, reservation.AccountID, reservation.Asset)
	if err != nil {
		return nil, decimal.Zero, err
	}
	releaseAmount := remaining
	if acct.BalanceLocked.LessThan(releaseAmount) {
		// Allow release to proceed with available locked funds to avoid blocking order flow.
		releaseAmount = acct.BalanceLocked
	}
	acct.BalanceLocked = acct.BalanceLocked.Sub(releaseAmount)
	acct.BalanceAvailable = acct.BalanceAvailable.Add(releaseAmount)
	now := time.Now().UTC()
	acct.UpdatedAt = now
	if _, err := tx.Exec(ctx, `
		UPDATE ledger_accounts
		SET balance_available = $1, balance_locked = $2, updated_at = $3
		WHERE id = $4
	`, acct.BalanceAvailable.String(), acct.BalanceLocked.String(), now, acct.ID); err != nil {
		return nil, decimal.Zero, err
	}

	consumed := reservation.Amount.Sub(releaseAmount)
	status := "released"
	if consumed.GreaterThanOrEqual(reservation.Amount) {
		status = "consumed"
	}
	if err := s.updateReservation(ctx, tx, reservation, consumed, status); err != nil {
		return nil, decimal.Zero, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, decimal.Zero, err
	}
	committed = true

	reservation.ConsumedAmount = consumed
	reservation.Status = status
	reservation.UpdatedAt = now
	return reservation, releaseAmount, nil
}

func (s *Store) GetOrderAccountID(ctx context.Context, orderID uuid.UUID) (uuid.UUID, error) {
	var accountID uuid.UUID
	row := s.pool.QueryRow(ctx, `SELECT account_id FROM orders WHERE id = $1`, orderID)
	if err := row.Scan(&accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: %s", ErrOrderNotFound, orderID)
		}
		return uuid.Nil, err
	}
	return accountID, nil
}

func (s *Store) ApplySettlement(ctx context.Context, req SettlementRequest) (*SettlementResult, error) {
	if req.Price.LessThanOrEqual(decimal.Zero) || req.Quantity.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("price and quantity must be positive")
	}
	baseAsset, quoteAsset, err := splitSymbol(req.Symbol)
	if err != nil {
		return nil, err
	}

	if s.feeClient == nil {
		return nil, fmt.Errorf("fee client not configured")
	}

	makerSide := strings.ToLower(strings.TrimSpace(req.MakerSide))
	if makerSide != "buy" && makerSide != "sell" {
		return nil, fmt.Errorf("maker_side must be buy or sell")
	}
	takerSide := oppositeSide(makerSide)

	feeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	makerFee, err := s.calculateFee(feeCtx, req.MakerAccountID, req.Symbol, makerSide, "maker", req.Quantity, req.Price)
	if err != nil {
		return nil, err
	}
	takerFee, err := s.calculateFee(feeCtx, req.TakerAccountID, req.Symbol, takerSide, "taker", req.Quantity, req.Price)
	if err != nil {
		return nil, err
	}

	notional := req.Price.Mul(req.Quantity)
	makerReservedAsset := baseAsset
	makerReservedAmount := req.Quantity
	takerReservedAsset := quoteAsset
	takerReservedAmount := notional
	if makerSide == "buy" {
		makerReservedAsset = quoteAsset
		makerReservedAmount = notional
		takerReservedAsset = baseAsset
		takerReservedAmount = req.Quantity
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if req.EventID != "" {
		processed, err := s.isEventProcessed(ctx, tx, req.EventID)
		if err != nil {
			return nil, err
		}
		if processed {
			if err := s.markEventProcessed(ctx, tx, req.EventID); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			committed = true
			return &SettlementResult{AlreadyProcessed: true}, nil
		}
	}

	duplicate, err := s.CheckDuplicateSettlement(ctx, tx, req.TradeID)
	if err != nil {
		return nil, err
	}
	if duplicate {
		if err := s.markEventProcessed(ctx, tx, req.EventID); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		committed = true
		return &SettlementResult{AlreadyProcessed: true}, nil
	}

	feeAccountID, err := s.getOrCreateFeeAccount(ctx, tx)
	if err != nil {
		return nil, err
	}

	pending := buildSettlementEntries(req.MakerAccountID, req.TakerAccountID, feeAccountID, makerSide, baseAsset, quoteAsset, notional, req.Quantity, makerFee, takerFee)
	if len(pending) == 0 {
		return nil, fmt.Errorf("no settlement entries generated")
	}

	balanceEntries := make([]pendingEntry, len(pending))
	copy(balanceEntries, pending)

	makerReservedCover, err := s.reservationCover(ctx, tx, req.MakerOrderID, makerReservedAsset, makerReservedAmount)
	if err != nil {
		return nil, err
	}
	takerReservedCover, err := s.reservationCover(ctx, tx, req.TakerOrderID, takerReservedAsset, takerReservedAmount)
	if err != nil {
		return nil, err
	}

	reservationRemaining := map[string]decimal.Decimal{}
	reservationInitial := map[string]decimal.Decimal{}
	if makerReservedCover.GreaterThan(decimal.Zero) {
		key := ledgerKey(req.MakerAccountID, makerReservedAsset)
		reservationRemaining[key] = makerReservedCover
		reservationInitial[key] = makerReservedCover
	}
	if takerReservedCover.GreaterThan(decimal.Zero) {
		key := ledgerKey(req.TakerAccountID, takerReservedAsset)
		reservationRemaining[key] = takerReservedCover
		reservationInitial[key] = takerReservedCover
	}
	forcedLockedKeys := map[string]struct{}{}
	if req.MakerOrderID == uuid.Nil && makerReservedAmount.GreaterThan(decimal.Zero) {
		forcedLockedKeys[ledgerKey(req.MakerAccountID, makerReservedAsset)] = struct{}{}
	}
	if req.TakerOrderID == uuid.Nil && takerReservedAmount.GreaterThan(decimal.Zero) {
		forcedLockedKeys[ledgerKey(req.TakerAccountID, takerReservedAsset)] = struct{}{}
	}

	useLockedFlags := make([]bool, len(balanceEntries))
	for idx := range balanceEntries {
		entry := &balanceEntries[idx]
		if entry.EntryType != "debit" || entry.Amount.LessThanOrEqual(decimal.Zero) {
			continue
		}
		key := ledgerKey(entry.AccountID, entry.Asset)
		if _, forced := forcedLockedKeys[key]; forced {
			useLockedFlags[idx] = true
		}
		remaining, ok := reservationRemaining[key]
		if !ok || remaining.LessThanOrEqual(decimal.Zero) {
			continue
		}
		useLockedFlags[idx] = true
		consume := remaining
		if entry.Amount.LessThan(consume) {
			consume = entry.Amount
		}
		if consume.GreaterThan(decimal.Zero) {
			left := remaining.Sub(consume)
			if left.LessThanOrEqual(decimal.Zero) {
				delete(reservationRemaining, key)
			} else {
				reservationRemaining[key] = left
			}
		}
	}

	reservationConsumed := make(map[string]decimal.Decimal, len(reservationInitial))
	for key, initial := range reservationInitial {
		remaining := reservationRemaining[key]
		consumed := initial.Sub(remaining)
		if consumed.LessThan(decimal.Zero) {
			consumed = decimal.Zero
		}
		reservationConsumed[key] = consumed
	}

	accountMap := make(map[string]*LedgerAccount)
	for idx := range pending {
		entry := &pending[idx]
		key := ledgerKey(entry.AccountID, entry.Asset)
		acct := accountMap[key]
		if acct == nil {
			acct, err = s.getOrCreateLedgerAccountForUpdate(ctx, tx, entry.AccountID, entry.Asset)
			if err != nil {
				return nil, err
			}
			accountMap[key] = acct
		}
		entry.LedgerAccountID = acct.ID
	}
	for idx := range balanceEntries {
		entry := &balanceEntries[idx]
		if entry.Amount.LessThanOrEqual(decimal.Zero) {
			continue
		}
		key := ledgerKey(entry.AccountID, entry.Asset)
		acct := accountMap[key]
		if acct == nil {
			acct, err = s.getOrCreateLedgerAccountForUpdate(ctx, tx, entry.AccountID, entry.Asset)
			if err != nil {
				return nil, err
			}
			accountMap[key] = acct
		}
		entry.LedgerAccountID = acct.ID
		useLocked := useLockedFlags[idx]
		if err := applyEntry(acct, entry.EntryType, entry.Amount, useLocked); err != nil {
			return nil, err
		}
	}

	if makerReservedCover.GreaterThan(decimal.Zero) {
		consumed := reservationConsumed[ledgerKey(req.MakerAccountID, makerReservedAsset)]
		if consumed.GreaterThan(decimal.Zero) {
			if err := s.consumeReservation(ctx, tx, req.MakerOrderID, makerReservedAsset, consumed); err != nil {
				return nil, err
			}
		}
	}
	if takerReservedCover.GreaterThan(decimal.Zero) {
		consumed := reservationConsumed[ledgerKey(req.TakerAccountID, takerReservedAsset)]
		if consumed.GreaterThan(decimal.Zero) {
			if err := s.consumeReservation(ctx, tx, req.TakerOrderID, takerReservedAsset, consumed); err != nil {
				return nil, err
			}
		}
	}

	now := time.Now().UTC()
	for _, acct := range accountMap {
		acct.UpdatedAt = now
		_, err := tx.Exec(ctx, `
			UPDATE ledger_accounts
			SET balance_available = $1, balance_locked = $2, updated_at = $3
			WHERE id = $4
		`, acct.BalanceAvailable.String(), acct.BalanceLocked.String(), now, acct.ID)
		if err != nil {
			return nil, err
		}
	}

	var entryIDs []uuid.UUID
	var entries []LedgerEntry
	for _, entry := range pending {
		entryID := uuid.New()
		_, err := tx.Exec(ctx, `
			INSERT INTO ledger_entries (id, ledger_account_id, entry_type, amount, reference_type, reference_id)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, entryID, entry.LedgerAccountID, entry.EntryType, entry.Amount.String(), referenceTypeTrade, req.TradeID)
		if err != nil {
			if isUniqueViolation(err) {
				if err := s.recordProcessedEvent(ctx, req.EventID); err != nil {
					return nil, err
				}
				return &SettlementResult{AlreadyProcessed: true}, nil
			}
			return nil, err
		}
		entryIDs = append(entryIDs, entryID)
		entries = append(entries, LedgerEntry{
			ID:              entryID,
			LedgerAccountID: entry.LedgerAccountID,
			AccountID:       entry.AccountID,
			Asset:           entry.Asset,
			EntryType:       entry.EntryType,
			Amount:          entry.Amount,
			ReferenceType:   referenceTypeTrade,
			ReferenceID:     req.TradeID,
			CreatedAt:       now,
		})
	}

	if req.EventID != "" {
		if err := s.markEventProcessed(ctx, tx, req.EventID); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	balances := make([]LedgerAccount, 0, len(accountMap))
	for _, acct := range accountMap {
		balances = append(balances, *acct)
	}

	return &SettlementResult{
		EntryIDs:     entryIDs,
		Entries:      entries,
		Balances:     balances,
		FeeAccountID: feeAccountID,
	}, nil
}

func (s *Store) CheckDuplicateSettlement(ctx context.Context, tx pgx.Tx, tradeID uuid.UUID) (bool, error) {
	var exists bool
	row := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM ledger_entries
			WHERE reference_type = $1 AND reference_id = $2
			LIMIT 1
		)
	`, referenceTypeTrade, tradeID)
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) GetEntriesByReference(ctx context.Context, referenceID uuid.UUID) ([]LedgerEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT le.id, le.ledger_account_id, la.account_id, la.asset, le.entry_type,
		       le.amount::text, le.reference_type, le.reference_id, le.created_at
		FROM ledger_entries le
		JOIN ledger_accounts la ON la.id = le.ledger_account_id
		WHERE le.reference_type = $1 AND le.reference_id = $2
		ORDER BY le.created_at ASC
	`, referenceTypeTrade, referenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LedgerEntry
	for rows.Next() {
		var entry LedgerEntry
		var amountStr string
		if err := rows.Scan(&entry.ID, &entry.LedgerAccountID, &entry.AccountID, &entry.Asset, &entry.EntryType, &amountStr, &entry.ReferenceType, &entry.ReferenceID, &entry.CreatedAt); err != nil {
			return nil, err
		}
		amt, err := decimal.NewFromString(amountStr)
		if err != nil {
			return nil, fmt.Errorf("parse entry amount: %w", err)
		}
		entry.Amount = amt
		entries = append(entries, entry)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return entries, nil
}

func (s *Store) GetBalancesByAccountAssets(ctx context.Context, assets []LedgerAccount) ([]LedgerAccount, error) {
	if len(assets) == 0 {
		return nil, nil
	}

	values := make([]string, 0, len(assets))
	args := make([]any, 0, len(assets)*2)
	for i, acct := range assets {
		values = append(values, fmt.Sprintf("($%d, $%d)", i*2+1, i*2+2))
		args = append(args, acct.AccountID, acct.Asset)
	}

	query := fmt.Sprintf(`
		SELECT id, account_id, asset, balance_available::text, balance_locked::text, updated_at
		FROM ledger_accounts
		WHERE (account_id, asset) IN (%s)
	`, strings.Join(values, ","))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var balances []LedgerAccount
	for rows.Next() {
		var acct LedgerAccount
		var availableStr, lockedStr string
		if err := rows.Scan(&acct.ID, &acct.AccountID, &acct.Asset, &availableStr, &lockedStr, &acct.UpdatedAt); err != nil {
			return nil, err
		}
		available, err := decimal.NewFromString(availableStr)
		if err != nil {
			return nil, fmt.Errorf("parse available balance: %w", err)
		}
		locked, err := decimal.NewFromString(lockedStr)
		if err != nil {
			return nil, fmt.Errorf("parse locked balance: %w", err)
		}
		acct.BalanceAvailable = available
		acct.BalanceLocked = locked
		balances = append(balances, acct)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return balances, nil
}

func (s *Store) GetFeeAccountID(ctx context.Context) (uuid.UUID, error) {
	var accountID uuid.UUID
	row := s.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE type = $1 ORDER BY created_at LIMIT 1`, feeAccountType)
	if err := row.Scan(&accountID); err != nil {
		return uuid.Nil, err
	}
	return accountID, nil
}

func (s *Store) calculateFee(ctx context.Context, accountID uuid.UUID, symbol, side, orderType string, quantity, price decimal.Decimal) (feeQuote, error) {
	side = strings.ToLower(strings.TrimSpace(side))
	if side != "buy" && side != "sell" {
		return feeQuote{}, fmt.Errorf("side must be buy or sell")
	}
	orderType = strings.ToLower(strings.TrimSpace(orderType))
	if orderType != "maker" && orderType != "taker" {
		return feeQuote{}, fmt.Errorf("order_type must be maker or taker")
	}

	if s.feeClient == nil {
		return feeQuote{}, fmt.Errorf("fee client not configured")
	}

	notional := quantity.Mul(price)
	feeAsset := quoteAsset(symbol)
	cacheKey := feeRateCacheKey(accountID, orderType, feeAsset)

	var lastErr error
	startOverall := time.Now()
	for attempt := 1; attempt <= 3; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
		attemptStart := time.Now()
		resp, err := s.feeClient.CalculateFees(attemptCtx, &feepb.CalculateFeesRequest{
			AccountId: accountID.String(),
			Symbol:    symbol,
			Side:      side,
			OrderType: orderType,
			Quantity:  quantity.String(),
			Price:     price.String(),
		})
		cancel()
		if err == nil && resp != nil && strings.TrimSpace(resp.FeeAsset) != "" {
			amount, err := decimal.NewFromString(resp.FeeAmount)
			if err != nil {
				return feeQuote{}, fmt.Errorf("parse fee amount: %w", err)
			}
			if amount.IsNegative() {
				return feeQuote{}, fmt.Errorf("fee amount must be non-negative")
			}
			if s.feeMetrics != nil {
				s.feeMetrics.ObserveFeeCall("success", time.Since(attemptStart))
			}
			if notional.GreaterThan(decimal.Zero) {
				rate := amount.Div(notional)
				if rate.GreaterThanOrEqual(decimal.Zero) {
					s.feeCache.set(cacheKey, rate, resp.FeeAsset)
				}
			}
			return feeQuote{
				Asset:  resp.FeeAsset,
				Amount: amount,
			}, nil
		}

		if err == nil && resp == nil {
			err = fmt.Errorf("fee response missing")
		}
		if err == nil && resp != nil && strings.TrimSpace(resp.FeeAsset) == "" {
			err = fmt.Errorf("fee asset missing")
		}

		lastErr = err
		retriable, reason := isFeeRetriable(err)
		if s.feeMetrics != nil {
			s.feeMetrics.ObserveFeeCall("error", time.Since(attemptStart))
			if retriable {
				s.feeMetrics.IncFeeRetry()
			} else {
				s.feeMetrics.IncFeeFailure(reason)
			}
		}
		if !retriable {
			return feeQuote{}, fmt.Errorf("calculate %s fee: %w", orderType, err)
		}
		if attempt < 3 {
			time.Sleep(backoffDuration(attempt))
		}
	}

	if s.feeMetrics != nil {
		s.feeMetrics.ObserveFeeCall("failure", time.Since(startOverall))
		s.feeMetrics.IncFeeFailure("unavailable")
	}
	if entry, ok := s.feeCache.get(cacheKey); ok && notional.GreaterThan(decimal.Zero) {
		amount := notional.Mul(entry.rate)
		if s.feeMetrics != nil {
			s.feeMetrics.IncFeeFallback("cached")
		}
		s.logger.Warn("fee service unavailable, using cached rate", "order_type", orderType, "account_id", accountID.String())
		return feeQuote{
			Asset:  entry.asset,
			Amount: amount,
		}, nil
	}

	if s.feeMetrics != nil {
		s.feeMetrics.IncFeeFallback("zero")
	}
	s.logger.Warn("fee service unavailable, using zero fee", "order_type", orderType, "account_id", accountID.String(), "error", lastErr)
	return feeQuote{
		Asset:  feeAsset,
		Amount: decimal.Zero,
	}, nil
}

func (s *Store) insertProcessedEvent(ctx context.Context, tx pgx.Tx, eventID string) (bool, error) {
	keys := ledgerEventKeys(eventID)
	if len(keys) == 0 {
		return false, nil
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id)
		SELECT unnest($1::text[])
		ON CONFLICT (event_id) DO NOTHING
	`, keys)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) markEventProcessed(ctx context.Context, tx pgx.Tx, eventID string) error {
	if len(ledgerEventKeys(eventID)) == 0 {
		return nil
	}
	_, err := s.insertProcessedEvent(ctx, tx, eventID)
	return err
}

func (s *Store) recordProcessedEvent(ctx context.Context, eventID string) error {
	if len(ledgerEventKeys(eventID)) == 0 {
		return nil
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if _, err := s.insertProcessedEvent(ctx, tx, eventID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) isEventProcessed(ctx context.Context, tx pgx.Tx, eventID string) (bool, error) {
	keys := ledgerEventKeys(eventID)
	if len(keys) == 0 {
		return false, nil
	}
	var exists bool
	row := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM processed_events WHERE event_id = ANY($1::text[])
		)
	`, keys)
	if err := row.Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func ledgerEventKey(eventID string) string {
	trimmed := strings.TrimSpace(eventID)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, ledgerEventPrefix) {
		return trimmed
	}
	return ledgerEventPrefix + trimmed
}

func ledgerEventKeys(eventID string) []string {
	trimmed := strings.TrimSpace(eventID)
	if trimmed == "" {
		return nil
	}
	keys := map[string]struct{}{}
	keys[trimmed] = struct{}{}
	if strings.HasPrefix(trimmed, ledgerEventPrefix) {
		raw := strings.TrimPrefix(trimmed, ledgerEventPrefix)
		if raw != "" {
			keys[raw] = struct{}{}
		}
	} else {
		keys[ledgerEventPrefix+trimmed] = struct{}{}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	return result
}

func (s *Store) getOrCreateLedgerAccountForUpdate(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, asset string) (*LedgerAccount, error) {
	acct, err := s.getLedgerAccountForUpdate(ctx, tx, accountID, asset)
	if err == nil {
		return acct, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_accounts (account_id, asset, balance_available, balance_locked)
		VALUES ($1, $2, 0, 0)
		ON CONFLICT (account_id, asset) DO NOTHING
	`, accountID, asset)
	if err != nil {
		return nil, err
	}

	return s.getLedgerAccountForUpdate(ctx, tx, accountID, asset)
}

func (s *Store) getLedgerAccountForUpdate(ctx context.Context, tx pgx.Tx, accountID uuid.UUID, asset string) (*LedgerAccount, error) {
	var acct LedgerAccount
	var availableStr, lockedStr string
	row := tx.QueryRow(ctx, `
		SELECT id, account_id, asset, balance_available::text, balance_locked::text, updated_at
		FROM ledger_accounts
		WHERE account_id = $1 AND asset = $2
		FOR UPDATE
	`, accountID, asset)
	if err := row.Scan(&acct.ID, &acct.AccountID, &acct.Asset, &availableStr, &lockedStr, &acct.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	acct.BalanceAvailable, err = decimal.NewFromString(availableStr)
	if err != nil {
		return nil, fmt.Errorf("parse available balance: %w", err)
	}
	acct.BalanceLocked, err = decimal.NewFromString(lockedStr)
	if err != nil {
		return nil, fmt.Errorf("parse locked balance: %w", err)
	}
	return &acct, nil
}

func (s *Store) getReservationForUpdate(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (*BalanceReservation, error) {
	var res BalanceReservation
	var amountStr, consumedStr string
	row := tx.QueryRow(ctx, `
		SELECT id, order_id, account_id, asset, amount::text, consumed_amount::text, status, created_at, updated_at
		FROM balance_reservations
		WHERE order_id = $1
		FOR UPDATE
	`, orderID)
	if err := row.Scan(&res.ID, &res.OrderID, &res.AccountID, &res.Asset, &amountStr, &consumedStr, &res.Status, &res.CreatedAt, &res.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	res.Amount, err = decimal.NewFromString(amountStr)
	if err != nil {
		return nil, fmt.Errorf("parse reservation amount: %w", err)
	}
	res.ConsumedAmount, err = decimal.NewFromString(consumedStr)
	if err != nil {
		return nil, fmt.Errorf("parse reservation consumed amount: %w", err)
	}
	return &res, nil
}

func (s *Store) getReservation(ctx context.Context, orderID uuid.UUID) (*BalanceReservation, error) {
	var res BalanceReservation
	var amountStr, consumedStr string
	row := s.pool.QueryRow(ctx, `
		SELECT id, order_id, account_id, asset, amount::text, consumed_amount::text, status, created_at, updated_at
		FROM balance_reservations
		WHERE order_id = $1
	`, orderID)
	if err := row.Scan(&res.ID, &res.OrderID, &res.AccountID, &res.Asset, &amountStr, &consumedStr, &res.Status, &res.CreatedAt, &res.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	res.Amount, err = decimal.NewFromString(amountStr)
	if err != nil {
		return nil, fmt.Errorf("parse reservation amount: %w", err)
	}
	res.ConsumedAmount, err = decimal.NewFromString(consumedStr)
	if err != nil {
		return nil, fmt.Errorf("parse reservation consumed amount: %w", err)
	}
	return &res, nil
}

func (s *Store) updateReservation(ctx context.Context, tx pgx.Tx, reservation *BalanceReservation, consumed decimal.Decimal, status string) error {
	if reservation == nil {
		return fmt.Errorf("reservation required")
	}
	now := time.Now().UTC()
	_, err := tx.Exec(ctx, `
		UPDATE balance_reservations
		SET consumed_amount = $1, status = $2, updated_at = $3
		WHERE id = $4
	`, consumed.String(), status, now, reservation.ID)
	return err
}

func (s *Store) reservationCover(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, asset string, desired decimal.Decimal) (decimal.Decimal, error) {
	if orderID == uuid.Nil || desired.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, nil
	}
	reservation, err := s.getReservationForUpdate(ctx, tx, orderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, nil
		}
		return decimal.Zero, err
	}
	if reservation.Status != "active" {
		return decimal.Zero, nil
	}
	if strings.ToUpper(reservation.Asset) != strings.ToUpper(strings.TrimSpace(asset)) {
		return decimal.Zero, fmt.Errorf("reservation asset mismatch")
	}
	remaining := reservation.Amount.Sub(reservation.ConsumedAmount)
	if remaining.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, nil
	}
	if desired.GreaterThan(remaining) {
		return remaining, nil
	}
	return desired, nil
}

func (s *Store) consumeReservation(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, asset string, amount decimal.Decimal) error {
	if orderID == uuid.Nil {
		return nil
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return nil
	}
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return nil
	}

	reservation, err := s.getReservationForUpdate(ctx, tx, orderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if strings.ToUpper(reservation.Asset) != asset {
		return fmt.Errorf("reservation asset mismatch")
	}
	if reservation.Status != "active" {
		return nil
	}

	remaining := reservation.Amount.Sub(reservation.ConsumedAmount)
	if remaining.LessThanOrEqual(decimal.Zero) {
		return nil
	}

	consume := amount
	if consume.GreaterThan(remaining) {
		consume = remaining
	}
	if consume.LessThanOrEqual(decimal.Zero) {
		return nil
	}

	newConsumed := reservation.ConsumedAmount.Add(consume)
	status := reservation.Status
	if newConsumed.GreaterThanOrEqual(reservation.Amount) {
		status = "consumed"
	}

	return s.updateReservation(ctx, tx, reservation, newConsumed, status)
}

func (s *Store) getOrCreateFeeAccount(ctx context.Context, tx pgx.Tx) (uuid.UUID, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(424242)); err != nil {
		return uuid.Nil, err
	}

	var accountID uuid.UUID
	row := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE type = $1 ORDER BY created_at LIMIT 1`, feeAccountType)
	if err := row.Scan(&accountID); err == nil {
		return accountID, nil
	}

	var userID uuid.UUID
	userRow := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, feeAccountEmail)
	if err := userRow.Scan(&userID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
		passwordHash := "$argon2id$v=19$m=65536,t=3,p=2$eWQxa2d4c0U1ZzA2c2ZxVg$O5UacC8gEAJye4Vt73Qb9ZypXeH0eBG34tLkR1I1wsQ"
		if err := tx.QueryRow(ctx, `
			INSERT INTO users (email, password_hash, status, kyc_level)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, feeAccountEmail, passwordHash, "system", "none").Scan(&userID); err != nil {
			return uuid.Nil, err
		}
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO accounts (user_id, type)
		VALUES ($1, $2)
		RETURNING id
	`, userID, feeAccountType).Scan(&accountID); err != nil {
		return uuid.Nil, err
	}

	return accountID, nil
}

type feeQuote struct {
	Asset  string
	Amount decimal.Decimal
}

type feeRateCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]feeRateCacheEntry
}

type feeRateCacheEntry struct {
	rate    decimal.Decimal
	asset   string
	expires time.Time
}

func newFeeRateCache(ttl time.Duration) *feeRateCache {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &feeRateCache{
		ttl:     ttl,
		entries: make(map[string]feeRateCacheEntry),
	}
}

func (c *feeRateCache) get(key string) (feeRateCacheEntry, bool) {
	if c == nil {
		return feeRateCacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return feeRateCacheEntry{}, false
	}
	if time.Now().After(entry.expires) {
		delete(c.entries, key)
		return feeRateCacheEntry{}, false
	}
	return entry, true
}

func (c *feeRateCache) set(key string, rate decimal.Decimal, asset string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = feeRateCacheEntry{
		rate:    rate,
		asset:   asset,
		expires: time.Now().Add(c.ttl),
	}
}

type pendingEntry struct {
	AccountID       uuid.UUID
	LedgerAccountID uuid.UUID
	Asset           string
	EntryType       string
	Amount          decimal.Decimal
}

func buildSettlementEntries(makerAccountID, takerAccountID, feeAccountID uuid.UUID, makerSide, baseAsset, quoteAsset string, notional, quantity decimal.Decimal, makerFee, takerFee feeQuote) []pendingEntry {
	var entries []pendingEntry

	makerBuys := makerSide == "buy"
	if makerBuys {
		entries = append(entries,
			pendingEntry{AccountID: makerAccountID, Asset: quoteAsset, EntryType: "debit", Amount: notional},
			pendingEntry{AccountID: makerAccountID, Asset: baseAsset, EntryType: "credit", Amount: quantity},
			pendingEntry{AccountID: takerAccountID, Asset: quoteAsset, EntryType: "credit", Amount: notional},
			pendingEntry{AccountID: takerAccountID, Asset: baseAsset, EntryType: "debit", Amount: quantity},
		)
	} else {
		entries = append(entries,
			pendingEntry{AccountID: makerAccountID, Asset: quoteAsset, EntryType: "credit", Amount: notional},
			pendingEntry{AccountID: makerAccountID, Asset: baseAsset, EntryType: "debit", Amount: quantity},
			pendingEntry{AccountID: takerAccountID, Asset: quoteAsset, EntryType: "debit", Amount: notional},
			pendingEntry{AccountID: takerAccountID, Asset: baseAsset, EntryType: "credit", Amount: quantity},
		)
	}

	if makerFee.Amount.GreaterThan(decimal.Zero) {
		entries = append(entries,
			pendingEntry{AccountID: makerAccountID, Asset: makerFee.Asset, EntryType: "debit", Amount: makerFee.Amount},
			pendingEntry{AccountID: feeAccountID, Asset: makerFee.Asset, EntryType: "credit", Amount: makerFee.Amount},
		)
	}
	if takerFee.Amount.GreaterThan(decimal.Zero) {
		entries = append(entries,
			pendingEntry{AccountID: takerAccountID, Asset: takerFee.Asset, EntryType: "debit", Amount: takerFee.Amount},
			pendingEntry{AccountID: feeAccountID, Asset: takerFee.Asset, EntryType: "credit", Amount: takerFee.Amount},
		)
	}

	return aggregateEntries(entries)
}

func applyEntry(acct *LedgerAccount, entryType string, amount decimal.Decimal, useLocked bool) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("amount must be positive")
	}
	switch entryType {
	case "debit":
		remaining := amount
		if useLocked && acct.BalanceLocked.GreaterThan(decimal.Zero) {
			if acct.BalanceLocked.GreaterThanOrEqual(remaining) {
				acct.BalanceLocked = acct.BalanceLocked.Sub(remaining)
				remaining = decimal.Zero
			} else {
				remaining = remaining.Sub(acct.BalanceLocked)
				acct.BalanceLocked = decimal.Zero
			}
		}
		if remaining.GreaterThan(decimal.Zero) {
			if acct.BalanceAvailable.LessThan(remaining) {
				return fmt.Errorf("insufficient balance for account %s asset %s", acct.AccountID, acct.Asset)
			}
			acct.BalanceAvailable = acct.BalanceAvailable.Sub(remaining)
		}
	case "credit":
		acct.BalanceAvailable = acct.BalanceAvailable.Add(amount)
	default:
		return fmt.Errorf("invalid entry type")
	}
	if acct.BalanceAvailable.IsNegative() || acct.BalanceLocked.IsNegative() {
		return fmt.Errorf("insufficient balance for account %s asset %s", acct.AccountID, acct.Asset)
	}
	return nil
}

type aggregatedEntry struct {
	AccountID uuid.UUID
	Asset     string
	Net       decimal.Decimal
}

func aggregateEntries(entries []pendingEntry) []pendingEntry {
	index := make(map[string]int, len(entries))
	ordered := make([]aggregatedEntry, 0, len(entries))

	for _, entry := range entries {
		key := ledgerKey(entry.AccountID, entry.Asset)
		delta := entry.Amount
		if entry.EntryType == "debit" {
			delta = delta.Neg()
		}
		if idx, ok := index[key]; ok {
			ordered[idx].Net = ordered[idx].Net.Add(delta)
			continue
		}
		index[key] = len(ordered)
		ordered = append(ordered, aggregatedEntry{
			AccountID: entry.AccountID,
			Asset:     entry.Asset,
			Net:       delta,
		})
	}

	result := make([]pendingEntry, 0, len(ordered))
	for _, agg := range ordered {
		if agg.Net.Equal(decimal.Zero) {
			continue
		}
		entryType := "credit"
		amount := agg.Net
		if agg.Net.IsNegative() {
			entryType = "debit"
			amount = agg.Net.Abs()
		}
		result = append(result, pendingEntry{
			AccountID: agg.AccountID,
			Asset:     agg.Asset,
			EntryType: entryType,
			Amount:    amount,
		})
	}

	return result
}

func splitSymbol(symbol string) (string, string, error) {
	trimmed := strings.TrimSpace(symbol)
	if trimmed == "" {
		return "", "", fmt.Errorf("symbol is required")
	}
	if strings.Contains(trimmed, "-") {
		parts := strings.Split(trimmed, "-")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], nil
		}
	}
	if strings.Contains(trimmed, "/") {
		parts := strings.Split(trimmed, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1], nil
		}
	}
	return "", "", fmt.Errorf("symbol must be in BASE-QUOTE format")
}

func quoteAsset(symbol string) string {
	_, quote, err := splitSymbol(symbol)
	if err != nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(quote))
}

func oppositeSide(side string) string {
	if side == "buy" {
		return "sell"
	}
	return "buy"
}

func ledgerKey(accountID uuid.UUID, asset string) string {
	return accountID.String() + ":" + strings.ToUpper(asset)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func feeRateCacheKey(accountID uuid.UUID, orderType, asset string) string {
	return accountID.String() + ":" + strings.ToLower(strings.TrimSpace(orderType)) + ":" + strings.ToUpper(strings.TrimSpace(asset))
}

func isFeeRetriable(err error) (bool, string) {
	if err == nil {
		return false, "none"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true, "timeout"
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable:
			return true, "unavailable"
		case codes.DeadlineExceeded:
			return true, "timeout"
		default:
			return false, st.Code().String()
		}
	}
	return false, "error"
}

func backoffDuration(attempt int) time.Duration {
	base := 100 * time.Millisecond
	if attempt <= 1 {
		return base
	}
	return base * time.Duration(attempt)
}
