package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/services/ledger/internal/storage"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type Store interface {
	GetBalance(ctx context.Context, accountID uuid.UUID, asset string) (storage.LedgerAccount, error)
	ApplySettlement(ctx context.Context, req storage.SettlementRequest) (*storage.SettlementResult, error)
	GetEntriesByReference(ctx context.Context, referenceID uuid.UUID) ([]storage.LedgerEntry, error)
}

type LedgerService struct {
	ledgerpb.UnimplementedLedgerServer
	store   Store
	logger  *slog.Logger
	metrics *Metrics
}

func NewLedgerService(store Store, logger *slog.Logger, metrics *Metrics) *LedgerService {
	if logger == nil {
		logger = slog.Default()
	}
	return &LedgerService{
		store:   store,
		logger:  logger,
		metrics: metrics,
	}
}

func (s *LedgerService) GetBalance(ctx context.Context, req *ledgerpb.GetBalanceRequest) (*ledgerpb.GetBalanceResponse, error) {
	accountID, err := parseUUID(req.GetAccountId(), "account_id")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	asset := strings.TrimSpace(req.GetAsset())
	if asset == "" {
		return nil, status.Error(codes.InvalidArgument, "asset is required")
	}

	balance, err := s.store.GetBalance(ctx, accountID, asset)
	if err != nil {
		if s.metrics != nil {
			s.metrics.BalanceLookups.WithLabelValues("error").Inc()
		}
		s.logger.Error("balance lookup failed", "error", err)
		return nil, status.Error(codes.Internal, "balance lookup failed")
	}

	if s.metrics != nil {
		s.metrics.BalanceLookups.WithLabelValues("success").Inc()
	}

	updatedAt := ""
	if !balance.UpdatedAt.IsZero() {
		updatedAt = balance.UpdatedAt.UTC().Format(time.RFC3339)
	}

	return &ledgerpb.GetBalanceResponse{
		AccountId: accountID.String(),
		Asset:     balance.Asset,
		Available: balance.BalanceAvailable.String(),
		Locked:    balance.BalanceLocked.String(),
		UpdatedAt: updatedAt,
	}, nil
}

func (s *LedgerService) ApplySettlement(ctx context.Context, req *ledgerpb.ApplySettlementRequest) (*ledgerpb.ApplySettlementResponse, error) {
	start := time.Now()
	result, err := s.applySettlement(ctx, req, req.GetTradeId())
	if s.metrics != nil {
		s.metrics.SettlementDuration.WithLabelValues("ApplySettlement").Observe(time.Since(start).Seconds())
	}
	if err != nil {
		return nil, err
	}

	entryIDs := make([]string, 0, len(result.EntryIDs))
	for _, id := range result.EntryIDs {
		entryIDs = append(entryIDs, id.String())
	}

	if result.AlreadyProcessed && len(entryIDs) == 0 {
		tradeID, err := parseUUID(req.GetTradeId(), "trade_id")
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		entries, err := s.store.GetEntriesByReference(ctx, tradeID)
		if err != nil {
			s.logger.Error("fetch duplicate entries failed", "trade_id", tradeID.String(), "error", err)
			return nil, status.Error(codes.Internal, "duplicate settlement lookup failed")
		}
		for _, entry := range entries {
			entryIDs = append(entryIDs, entry.ID.String())
		}
		if len(entryIDs) == 0 {
			s.logger.Error("duplicate settlement missing entries", "trade_id", tradeID.String())
			return nil, status.Error(codes.Internal, "duplicate settlement missing entries")
		}
	}

	if s.metrics != nil {
		statusLabel := "success"
		if result.AlreadyProcessed {
			statusLabel = "duplicate"
		}
		s.metrics.SettlementsTotal.WithLabelValues(statusLabel).Inc()
	}

	return &ledgerpb.ApplySettlementResponse{
		Success:        true,
		LedgerEntryIds: entryIDs,
	}, nil
}

func (s *LedgerService) ApplySettlementInternal(ctx context.Context, req *ledgerpb.ApplySettlementRequest, eventID string) (*storage.SettlementResult, error) {
	return s.applySettlement(ctx, req, eventID)
}

func (s *LedgerService) applySettlement(ctx context.Context, req *ledgerpb.ApplySettlementRequest, eventID string) (*storage.SettlementResult, error) {
	tradeID, err := parseUUID(req.GetTradeId(), "trade_id")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	makerAccountID, err := parseUUID(req.GetMakerAccountId(), "maker_account_id")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	takerAccountID, err := parseUUID(req.GetTakerAccountId(), "taker_account_id")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	symbol := strings.TrimSpace(req.GetSymbol())
	if symbol == "" {
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}
	makerSide := strings.ToLower(strings.TrimSpace(req.GetMakerSide()))
	if makerSide != "buy" && makerSide != "sell" {
		return nil, status.Error(codes.InvalidArgument, "maker_side must be buy or sell")
	}

	price, err := parsePositiveDecimal(req.GetPrice(), "price")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	quantity, err := parsePositiveDecimal(req.GetQuantity(), "quantity")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	result, err := s.store.ApplySettlement(ctx, storage.SettlementRequest{
		TradeID:        tradeID,
		MakerAccountID: makerAccountID,
		TakerAccountID: takerAccountID,
		Symbol:         symbol,
		Price:          price,
		Quantity:       quantity,
		MakerSide:      makerSide,
		EventID:        strings.TrimSpace(eventID),
	})
	if err != nil {
		if s.metrics != nil {
			s.metrics.SettlementsTotal.WithLabelValues("error").Inc()
			s.metrics.SettlementErrors.WithLabelValues("apply").Inc()
		}
		s.logger.Error("settlement failed", "error", err)
		return nil, status.Error(codes.Internal, "settlement failed")
	}

	return result, nil
}

func parseUUID(value, field string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return uuid.Nil, fmt.Errorf("%s is required", field)
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s", field)
	}
	return parsed, nil
}

func parsePositiveDecimal(value, field string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return decimal.Zero, fmt.Errorf("%s is required", field)
	}
	dec, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%s must be a decimal", field)
	}
	if dec.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("%s must be positive", field)
	}
	return dec, nil
}
