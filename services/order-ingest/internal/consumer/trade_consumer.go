package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/service"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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
	ApplyTradeExecution(ctx context.Context, eventID string, fills []storage.OrderFill) (bool, error)
}

type TradeConsumer struct {
	store   Store
	logger  *slog.Logger
	metrics *service.Metrics
}

func NewTradeConsumer(store Store, logger *slog.Logger, metrics *service.Metrics) *TradeConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &TradeConsumer{store: store, logger: logger, metrics: metrics}
}

func (c *TradeConsumer) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if msg == nil || len(msg.Value) == 0 {
		c.record("error")
		return fmt.Errorf("empty kafka message")
	}

	var event TradeExecutedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		c.record("error")
		return fmt.Errorf("decode trades.executed: %w", err)
	}
	if err := event.Validate(); err != nil {
		c.record("error")
		return err
	}

	makerOrderID, err := parseUUID(event.MakerOrderID, "maker_order_id")
	if err != nil {
		c.record("error")
		return err
	}
	takerOrderID, err := parseUUID(event.TakerOrderID, "taker_order_id")
	if err != nil {
		c.record("error")
		return err
	}

	qty, err := decimal.NewFromString(strings.TrimSpace(event.Quantity))
	if err != nil {
		c.record("error")
		return fmt.Errorf("invalid quantity")
	}

	fills := []storage.OrderFill{
		{OrderID: makerOrderID, Quantity: qty},
		{OrderID: takerOrderID, Quantity: qty},
	}

	already, err := c.store.ApplyTradeExecution(ctx, event.EventID, fills)
	if err != nil {
		c.record("error")
		return err
	}
	if already {
		c.logger.Info("trade event already processed", "event_id", event.EventID, "trade_id", event.TradeID)
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
