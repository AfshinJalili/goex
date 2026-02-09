package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AfshinJalili/goex/services/risk/internal/storage"
	riskpb "github.com/AfshinJalili/goex/services/risk/proto/risk/v1"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

type Store interface {
	GetAccountInfo(ctx context.Context, accountID uuid.UUID) (*storage.AccountInfo, error)
	GetMarketBySymbol(ctx context.Context, symbol string) (*storage.Market, error)
	CheckBalance(ctx context.Context, accountID uuid.UUID, asset string, required decimal.Decimal) (*storage.BalanceCheck, error)
	GetLastTradePrice(ctx context.Context, symbol string) (decimal.Decimal, error)
}

type MarketCache interface {
	GetMarket(symbol string) (*storage.Market, bool)
}

type RiskService struct {
	riskpb.UnimplementedRiskServer
	store                Store
	cache                MarketCache
	logger               *slog.Logger
	metrics              *Metrics
	marketBuySlippageBps int
	balanceBreaker       *circuitBreaker
}

func NewRiskService(store Store, cache MarketCache, logger *slog.Logger, metrics *Metrics, marketBuySlippageBps int) *RiskService {
	if logger == nil {
		logger = slog.Default()
	}
	if marketBuySlippageBps < 0 {
		marketBuySlippageBps = 0
	}
	return &RiskService{
		store:                store,
		cache:                cache,
		logger:               logger,
		metrics:              metrics,
		marketBuySlippageBps: marketBuySlippageBps,
		balanceBreaker:       newCircuitBreaker(3, 30*time.Second),
	}
}

func (s *RiskService) PreTradeCheck(ctx context.Context, req *riskpb.PreTradeCheckRequest) (*riskpb.PreTradeCheckResponse, error) {
	start := time.Now()

	accountID, err := parseUUID(req.GetAccountId(), "account_id")
	if err != nil {
		s.recordMetrics("denied", "invalid", start)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	symbol := strings.TrimSpace(req.GetSymbol())
	if symbol == "" {
		s.recordMetrics("denied", "invalid", start)
		return nil, status.Error(codes.InvalidArgument, "symbol is required")
	}
	normalizedSymbol := strings.ToUpper(symbol)

	side := strings.ToLower(strings.TrimSpace(req.GetSide()))
	if side != "buy" && side != "sell" {
		s.recordMetrics("denied", "invalid", start)
		return nil, status.Error(codes.InvalidArgument, "side must be buy or sell")
	}

	orderType := strings.ToLower(strings.TrimSpace(req.GetOrderType()))
	if orderType != "limit" && orderType != "market" {
		s.recordMetrics("denied", "invalid", start)
		return nil, status.Error(codes.InvalidArgument, "order_type must be limit or market")
	}

	quantity, err := parsePositiveDecimal(req.GetQuantity(), "quantity")
	if err != nil {
		s.recordMetrics("denied", "invalid", start)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	price := decimal.Zero
	priceRaw := strings.TrimSpace(req.GetPrice())
	if orderType == "limit" {
		parsed, err := parsePositiveDecimal(priceRaw, "price")
		if err != nil {
			s.recordMetrics("denied", "invalid", start)
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		price = parsed
	}
	if orderType == "market" && priceRaw != "" {
		parsed, err := parsePositiveDecimal(priceRaw, "price")
		if err != nil {
			s.recordMetrics("denied", "invalid", start)
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		price = parsed
	}

	accountInfo, err := s.store.GetAccountInfo(ctx, accountID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.recordMetrics("denied", "account_status", start)
			return nil, status.Error(codes.NotFound, "account not found")
		}
		s.logger.Error("account lookup failed", "account_id", accountID.String(), "error", err)
		s.recordMetrics("denied", "account_status", start)
		return nil, status.Error(codes.Internal, "account lookup failed")
	}

	denialReasons := make([]string, 0)
	if strings.ToLower(strings.TrimSpace(accountInfo.Status)) != "active" {
		denialReasons = append(denialReasons, "account_inactive")
	}

	kyc := strings.ToLower(strings.TrimSpace(accountInfo.KYCLevel))
	if kyc != "verified" && kyc != "approved" {
		denialReasons = append(denialReasons, "kyc_insufficient")
	}

	market, ok := s.cache.GetMarket(normalizedSymbol)
	if ok {
		s.recordCacheHit("hit")
	} else {
		s.recordCacheHit("miss")
		market, err = s.store.GetMarketBySymbol(ctx, normalizedSymbol)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				denialReasons = append(denialReasons, "market_not_found")
			} else {
				s.logger.Error("market lookup failed", "symbol", symbol, "error", err)
				s.recordMetrics("denied", "market_status", start)
				return nil, status.Error(codes.Internal, "market lookup failed")
			}
		}
	}

	if market != nil {
		if strings.ToLower(strings.TrimSpace(market.Status)) != "active" {
			denialReasons = append(denialReasons, "market_inactive")
		}
	}

	details := map[string]string{}
	if len(denialReasons) == 0 {
		requiredAsset := ""
		var requiredAmount decimal.Decimal

		switch side {
		case "sell":
			requiredAsset = market.BaseAsset
			requiredAmount = quantity
		case "buy":
			requiredAsset = market.QuoteAsset
			if orderType == "market" {
				refPrice, err := s.marketBuyReferencePrice(ctx, normalizedSymbol)
				if err != nil {
					denialReasons = append(denialReasons, "market_price_unavailable")
					details["required_asset"] = requiredAsset
					break
				}
				requiredAmount = quantity.Mul(refPrice)
				details["reference_price"] = refPrice.String()
			} else {
				requiredAmount = quantity.Mul(price)
			}
		}

		if requiredAsset != "" && len(denialReasons) == 0 {
			if s.balanceBreaker != nil && !s.balanceBreaker.Allow() {
				s.recordMetrics("denied", "balance", start)
				return nil, status.Error(codes.Unavailable, "balance service unavailable")
			}
			balance, err := s.store.CheckBalance(ctx, accountID, requiredAsset, requiredAmount)
			if err != nil {
				if s.balanceBreaker != nil {
					s.balanceBreaker.RecordFailure()
				}
				s.logger.Error("balance check failed", "account_id", accountID.String(), "asset", requiredAsset, "error", err)
				s.recordMetrics("denied", "balance", start)
				return nil, status.Error(codes.Internal, "balance check failed")
			}
			if s.balanceBreaker != nil {
				s.balanceBreaker.RecordSuccess()
			}
			if !balance.Sufficient {
				denialReasons = append(denialReasons, "insufficient_balance")
				details["required_asset"] = requiredAsset
				details["required_amount"] = requiredAmount.String()
				details["available"] = balance.Available.String()
			}
		}
	}

	allowed := len(denialReasons) == 0
	if allowed {
		s.recordMetrics("allowed", "none", start)
	} else {
		for _, reason := range denialReasons {
			s.recordReasonMetrics(reason)
		}
		s.recordLatency("denied", start)
	}

	if !allowed {
		s.logger.Info("pre-trade check denied", "account_id", accountID.String(), "symbol", normalizedSymbol, "reasons", denialReasons)
	}

	return &riskpb.PreTradeCheckResponse{
		Allowed: allowed,
		Reasons: denialReasons,
		Details: details,
	}, nil
}

func (s *RiskService) recordCacheHit(status string) {
	if s.metrics == nil {
		return
	}
	s.metrics.CacheHits.WithLabelValues(status).Inc()
}

func (s *RiskService) recordMetrics(result, reason string, start time.Time) {
	if s.metrics == nil {
		return
	}
	s.metrics.PreTradeChecks.WithLabelValues(result, reason).Inc()
	s.metrics.PreTradeCheckLatency.WithLabelValues(result).Observe(time.Since(start).Seconds())
}

func (s *RiskService) recordReasonMetrics(reason string) {
	if s.metrics == nil {
		return
	}
	category := reasonCategory(reason)
	s.metrics.PreTradeChecks.WithLabelValues("denied", category).Inc()
}

func (s *RiskService) recordLatency(result string, start time.Time) {
	if s.metrics == nil {
		return
	}
	s.metrics.PreTradeCheckLatency.WithLabelValues(result).Observe(time.Since(start).Seconds())
}

func reasonCategory(reason string) string {
	switch reason {
	case "account_inactive":
		return "account_status"
	case "kyc_insufficient":
		return "kyc"
	case "market_not_found", "market_inactive":
		return "market_status"
	case "market_price_unavailable":
		return "market_status"
	case "insufficient_balance":
		return "balance"
	default:
		return "invalid"
	}
}

func (s *RiskService) marketBuyReferencePrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	if s.store == nil {
		return decimal.Zero, fmt.Errorf("store not configured")
	}
	price, err := s.store.GetLastTradePrice(ctx, symbol)
	if err != nil {
		return decimal.Zero, err
	}
	if price.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, fmt.Errorf("reference price unavailable")
	}
	slippage := decimal.NewFromInt(int64(s.marketBuySlippageBps)).Div(decimal.NewFromInt(10000))
	factor := decimal.NewFromInt(1).Add(slippage)
	return price.Mul(factor), nil
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

type circuitBreaker struct {
	mu          sync.Mutex
	failures    int
	threshold   int
	openedUntil time.Time
	cooldown    time.Duration
}

func newCircuitBreaker(threshold int, cooldown time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &circuitBreaker{
		threshold: threshold,
		cooldown:  cooldown,
	}
}

func (b *circuitBreaker) Allow() bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedUntil.IsZero() {
		return true
	}
	if time.Now().After(b.openedUntil) {
		b.openedUntil = time.Time{}
		b.failures = 0
		return true
	}
	return false
}

func (b *circuitBreaker) RecordSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedUntil = time.Time{}
}

func (b *circuitBreaker) RecordFailure() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openedUntil = time.Now().Add(b.cooldown)
	}
}
