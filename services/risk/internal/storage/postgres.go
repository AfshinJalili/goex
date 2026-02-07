package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
)

var ErrNotFound = errors.New("not found")

type LedgerClient interface {
	GetBalance(ctx context.Context, in *ledgerpb.GetBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.GetBalanceResponse, error)
}

type Store struct {
	pool         *pgxpool.Pool
	ledgerClient LedgerClient
}

func New(pool *pgxpool.Pool, ledgerClient LedgerClient) *Store {
	return &Store{pool: pool, ledgerClient: ledgerClient}
}

func (s *Store) GetAccountInfo(ctx context.Context, accountID uuid.UUID) (*AccountInfo, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT a.id, a.user_id, a.status, u.kyc_level
		FROM accounts a
		JOIN users u ON u.id = a.user_id
		WHERE a.id = $1
	`, accountID)

	var info AccountInfo
	if err := row.Scan(&info.ID, &info.UserID, &info.Status, &info.KYCLevel); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &info, nil
}

func (s *Store) GetMarketBySymbol(ctx context.Context, symbol string) (*Market, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	row := s.pool.QueryRow(ctx, `
		SELECT id, symbol, base_asset, quote_asset, status
		FROM markets
		WHERE symbol = $1
	`, symbol)

	var market Market
	if err := row.Scan(&market.ID, &market.Symbol, &market.BaseAsset, &market.QuoteAsset, &market.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &market, nil
}

func (s *Store) ListActiveMarkets(ctx context.Context) ([]Market, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, symbol, base_asset, quote_asset, status
		FROM markets
		WHERE status = 'active'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var markets []Market
	for rows.Next() {
		var market Market
		if err := rows.Scan(&market.ID, &market.Symbol, &market.BaseAsset, &market.QuoteAsset, &market.Status); err != nil {
			return nil, err
		}
		markets = append(markets, market)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return markets, nil
}

func (s *Store) CheckBalance(ctx context.Context, accountID uuid.UUID, asset string, required decimal.Decimal) (*BalanceCheck, error) {
	if s.ledgerClient == nil {
		return nil, fmt.Errorf("ledger client not configured")
	}

	resp, err := s.ledgerClient.GetBalance(ctx, &ledgerpb.GetBalanceRequest{
		AccountId: accountID.String(),
		Asset:     asset,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("ledger response missing")
	}

	availableStr := strings.TrimSpace(resp.GetAvailable())
	if availableStr == "" {
		return nil, fmt.Errorf("ledger available balance missing")
	}
	available, err := decimal.NewFromString(availableStr)
	if err != nil {
		return nil, fmt.Errorf("parse available balance: %w", err)
	}

	return &BalanceCheck{
		Asset:      asset,
		Available:  available,
		Required:   required,
		Sufficient: available.GreaterThanOrEqual(required),
	}, nil
}
