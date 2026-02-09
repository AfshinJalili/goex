package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/service"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log/slog"
)

const tradesExecutedEventType = "trades.executed"

type TradeExecutedEvent struct {
	kafka.Envelope
	TradeID      string `json:"trade_id"`
	Symbol       string `json:"symbol"`
	MakerOrderID string `json:"maker_order_id"`
	TakerOrderID string `json:"taker_order_id"`
	Price        string `json:"price"`
	Quantity     string `json:"quantity"`
	MakerSide    string `json:"maker_side"`
	ExecutedAt   string `json:"executed_at"`
}

type Store interface {
	ApplyTradeExecution(ctx context.Context, eventID string, fills []storage.OrderFill) (storage.ApplyTradeResult, error)
}

type LedgerClient interface {
	ReleaseBalance(ctx context.Context, in *ledgerpb.ReleaseBalanceRequest, opts ...grpc.CallOption) (*ledgerpb.ReleaseBalanceResponse, error)
}

type TradeConsumer struct {
	store   Store
	ledger  LedgerClient
	logger  *slog.Logger
	metrics *service.Metrics
}

func NewTradeConsumer(store Store, ledger LedgerClient, logger *slog.Logger, metrics *service.Metrics) *TradeConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &TradeConsumer{store: store, ledger: ledger, logger: logger, metrics: metrics}
}

func (c *TradeConsumer) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if msg == nil || len(msg.Value) == 0 {
		c.record("error")
		return kafka.DLQ(fmt.Errorf("empty kafka message"), "empty_message")
	}

	var event TradeExecutedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		c.record("error")
		return kafka.DLQ(fmt.Errorf("decode trades.executed: %w", err), "decode")
	}
	if err := event.Validate(); err != nil {
		c.record("error")
		return kafka.DLQ(err, "invalid_event")
	}

	makerOrderID, err := parseUUID(event.MakerOrderID, "maker_order_id")
	if err != nil {
		c.record("error")
		return kafka.DLQ(err, "invalid_event")
	}
	takerOrderID, err := parseUUID(event.TakerOrderID, "taker_order_id")
	if err != nil {
		c.record("error")
		return kafka.DLQ(err, "invalid_event")
	}

	qty, err := decimal.NewFromString(strings.TrimSpace(event.Quantity))
	if err != nil {
		c.record("error")
		return kafka.DLQ(fmt.Errorf("invalid quantity"), "invalid_event")
	}

	fills := []storage.OrderFill{
		{OrderID: makerOrderID, Quantity: qty},
		{OrderID: takerOrderID, Quantity: qty},
	}

	result, err := c.store.ApplyTradeExecution(ctx, event.EventID, fills)
	if err != nil {
		c.record("error")
		return err
	}
	if result.AlreadyProcessed {
		c.logger.Info("trade event already processed", "event_id", event.EventID, "trade_id", event.TradeID)
	}

	if len(result.FilledOrderIDs) > 0 {
		if c.ledger == nil {
			c.record("error")
			return fmt.Errorf("ledger client not configured")
		}
		for _, orderID := range result.FilledOrderIDs {
			releaseCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			_, err := c.ledger.ReleaseBalance(releaseCtx, &ledgerpb.ReleaseBalanceRequest{OrderId: orderID.String()})
			cancel()
			if err != nil {
				if status.Code(err) == codes.NotFound {
					c.logger.Warn("reservation not found on release", "order_id", orderID.String())
					continue
				}
				c.record("error")
				return err
			}
		}
	}

	c.record("success")
	return nil
}

func (e *TradeExecutedEvent) Validate() error {
	if err := e.Envelope.Validate(); err != nil {
		return err
	}
	if e.EventType != tradesExecutedEventType {
		return fmt.Errorf("unexpected event_type: %s", e.EventType)
	}
	if strings.TrimSpace(e.TradeID) == "" {
		return fmt.Errorf("trade_id is required")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return fmt.Errorf("symbol is required")
	}
	if strings.TrimSpace(e.MakerOrderID) == "" {
		return fmt.Errorf("maker_order_id is required")
	}
	if strings.TrimSpace(e.TakerOrderID) == "" {
		return fmt.Errorf("taker_order_id is required")
	}
	if strings.TrimSpace(e.Price) == "" {
		return fmt.Errorf("price is required")
	}
	if strings.TrimSpace(e.Quantity) == "" {
		return fmt.Errorf("quantity is required")
	}
	if _, err := decimal.NewFromString(strings.TrimSpace(e.Price)); err != nil {
		return fmt.Errorf("price must be decimal")
	}
	if _, err := decimal.NewFromString(strings.TrimSpace(e.Quantity)); err != nil {
		return fmt.Errorf("quantity must be decimal")
	}
	side := strings.ToLower(strings.TrimSpace(e.MakerSide))
	if side != "buy" && side != "sell" {
		return fmt.Errorf("maker_side must be buy or sell")
	}
	if e.ExecutedAt != "" {
		if _, err := time.Parse(time.RFC3339, e.ExecutedAt); err != nil {
			return fmt.Errorf("executed_at must be RFC3339")
		}
	}
	return nil
}

func (c *TradeConsumer) record(status string) {
	if c.metrics == nil {
		return
	}
	c.metrics.TradeEventsProcessed.WithLabelValues(status).Inc()
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
