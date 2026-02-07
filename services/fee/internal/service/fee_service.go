package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/services/fee/internal/storage"
	feepb "github.com/AfshinJalili/goex/services/fee/proto/fee/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type TierStore interface {
	GetFeeTierByVolume(ctx context.Context, volume string) (*storage.FeeTier, error)
	GetDefaultFeeTier(ctx context.Context) (*storage.FeeTier, error)
	GetAccountVolume30d(ctx context.Context, accountID uuid.UUID) (string, error)
}

type TierCache interface {
	GetTierByVolume(volume string) (*storage.FeeTier, bool)
	Size() int
}

type FeeService struct {
	feepb.UnimplementedFeeServiceServer
	store   TierStore
	cache   TierCache
	logger  *slog.Logger
	metrics *Metrics
}

func NewFeeService(store TierStore, cache TierCache, logger *slog.Logger, metrics *Metrics) *FeeService {
	return &FeeService{
		store:   store,
		cache:   cache,
		logger:  logger,
		metrics: metrics,
	}
}

func (s *FeeService) GetFeeTier(ctx context.Context, req *feepb.GetFeeTierRequest) (*feepb.GetFeeTierResponse, error) {
	accountID, err := parseAccountID(req.GetAccountId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	volume := strings.TrimSpace(req.GetVolume())
	if volume == "" {
		volume, err = s.store.GetAccountVolume30d(ctx, accountID)
		if err != nil {
			s.logger.Error("volume lookup failed", "error", err)
			return nil, status.Error(codes.Internal, "failed to lookup volume")
		}
	} else {
		parsed, err := parseNonNegativeDecimal(volume, "volume")
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		volume = parsed.String()
	}

	tier, fromCache, err := s.resolveTier(ctx, volume)
	if err != nil {
		return nil, err
	}

	if s.metrics != nil {
		statusLabel := "miss"
		if fromCache {
			statusLabel = "hit"
		}
		s.metrics.TierLookups.WithLabelValues(tier.Name, statusLabel).Inc()
	}

	return &feepb.GetFeeTierResponse{
		TierId:      tier.ID.String(),
		TierName:    tier.Name,
		MakerFeeBps: int32(tier.MakerFeeBps),
		TakerFeeBps: int32(tier.TakerFeeBps),
	}, nil
}

func (s *FeeService) CalculateFees(ctx context.Context, req *feepb.CalculateFeesRequest) (*feepb.CalculateFeesResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.FeeCalcDuration.WithLabelValues("CalculateFees").Observe(time.Since(start).Seconds())
		}
	}()

	accountID, err := parseAccountID(req.GetAccountId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	orderType := strings.ToLower(strings.TrimSpace(req.GetOrderType()))
	if orderType != "maker" && orderType != "taker" {
		if s.metrics != nil {
			s.metrics.FeeCalculations.WithLabelValues("invalid").Inc()
		}
		return nil, status.Error(codes.InvalidArgument, "order_type must be maker or taker")
	}

	symbol := strings.TrimSpace(req.GetSymbol())
	feeAsset, err := quoteAsset(symbol)
	if err != nil {
		if s.metrics != nil {
			s.metrics.FeeCalculations.WithLabelValues(orderType).Inc()
		}
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	quantity, err := parsePositiveDecimal(req.GetQuantity(), "quantity")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	price, err := parsePositiveDecimal(req.GetPrice(), "price")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	volume, err := s.store.GetAccountVolume30d(ctx, accountID)
	if err != nil {
		s.logger.Error("volume lookup failed", "error", err)
		return nil, status.Error(codes.Internal, "failed to lookup volume")
	}

	tier, fromCache, err := s.resolveTier(ctx, volume)
	if err != nil {
		return nil, err
	}

	if s.metrics != nil {
		statusLabel := "miss"
		if fromCache {
			statusLabel = "hit"
		}
		s.metrics.TierLookups.WithLabelValues(tier.Name, statusLabel).Inc()
		s.metrics.FeeCalculations.WithLabelValues(orderType).Inc()
	}

	bps := tier.MakerFeeBps
	if orderType == "taker" {
		bps = tier.TakerFeeBps
	}

	notional := quantity.Mul(price)
	feeRate := decimal.NewFromInt(int64(bps)).Div(decimal.NewFromInt(10000))
	feeAmount := notional.Mul(feeRate)

	return &feepb.CalculateFeesResponse{
		FeeAmount:   feeAmount.String(),
		FeeAsset:    feeAsset,
		TierApplied: tier.Name,
	}, nil
}

func (s *FeeService) resolveTier(ctx context.Context, volume string) (*storage.FeeTier, bool, error) {
	if tier, ok := s.cache.GetTierByVolume(volume); ok {
		return tier, true, nil
	}

	tier, err := s.store.GetFeeTierByVolume(ctx, volume)
	if err != nil {
		s.logger.Error("fee tier lookup failed", "error", err)
		return nil, false, status.Error(codes.Internal, "fee tier lookup failed")
	}
	if tier != nil {
		return tier, false, nil
	}

	defaultTier, err := s.store.GetDefaultFeeTier(ctx)
	if err != nil {
		s.logger.Error("default fee tier lookup failed", "error", err)
		return nil, false, status.Error(codes.Internal, "default fee tier lookup failed")
	}
	if defaultTier == nil {
		return nil, false, status.Error(codes.NotFound, "fee tier not found")
	}
	return defaultTier, false, nil
}

func parseAccountID(val string) (uuid.UUID, error) {
	id := strings.TrimSpace(val)
	if id == "" {
		return uuid.Nil, fmt.Errorf("account_id is required")
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid account_id")
	}
	return parsed, nil
}

func parsePositiveDecimal(val, field string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(val)
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

func parseNonNegativeDecimal(val, field string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return decimal.Zero, fmt.Errorf("%s is required", field)
	}
	dec, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%s must be a decimal", field)
	}
	if dec.IsNegative() {
		return decimal.Zero, fmt.Errorf("%s must be non-negative", field)
	}
	return dec, nil
}

func quoteAsset(symbol string) (string, error) {
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}
	if strings.Contains(symbol, "-") {
		parts := strings.Split(symbol, "-")
		if len(parts) == 2 && parts[1] != "" {
			return parts[1], nil
		}
	}
	if strings.Contains(symbol, "/") {
		parts := strings.Split(symbol, "/")
		if len(parts) == 2 && parts[1] != "" {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("symbol must be in BASE-QUOTE format")
}
