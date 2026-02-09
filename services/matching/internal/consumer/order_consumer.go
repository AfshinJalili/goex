package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/services/matching/internal/engine"
	"github.com/IBM/sarama"
	"github.com/shopspring/decimal"
	"log/slog"
)

const (
	ordersAcceptedEventType  = "orders.accepted"
	ordersCancelledEventType = "orders.cancelled"
)

type OrderAcceptedEvent struct {
	kafka.Envelope
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	AccountID     string `json:"account_id"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Price         string `json:"price"`
	Quantity      string `json:"quantity"`
	TimeInForce   string `json:"time_in_force"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

type OrderCancelledEvent struct {
	kafka.Envelope
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	AccountID     string `json:"account_id"`
	Symbol        string `json:"symbol"`
	Status        string `json:"status"`
	CancelledAt   string `json:"cancelled_at"`
}

type Engine interface {
	ProcessOrder(ctx context.Context, order *engine.Order, correlationID string) ([]engine.Trade, error)
	CancelOrder(orderID, symbol string) bool
}

type OrderConsumer struct {
	engine         Engine
	logger         *slog.Logger
	deduper        *eventDeduper
	orderDeduper   *eventDeduper
	acceptedTopic  string
	cancelledTopic string
}

func NewOrderConsumer(engine Engine, logger *slog.Logger, acceptedTopic, cancelledTopic string) *OrderConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(acceptedTopic) == "" {
		acceptedTopic = ordersAcceptedEventType
	}
	if strings.TrimSpace(cancelledTopic) == "" {
		cancelledTopic = ordersCancelledEventType
	}
	return &OrderConsumer{
		engine:         engine,
		logger:         logger,
		deduper:        newEventDeduper(100000),
		orderDeduper:   newEventDeduper(200000),
		acceptedTopic:  acceptedTopic,
		cancelledTopic: cancelledTopic,
	}
}

func (c *OrderConsumer) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if msg == nil || len(msg.Value) == 0 {
		return kafka.DLQ(fmt.Errorf("empty kafka message"), "empty_message")
	}

	switch msg.Topic {
	case c.acceptedTopic:
		return c.handleOrderAccepted(ctx, msg.Value)
	case c.cancelledTopic:
		return c.handleOrderCancelled(ctx, msg.Value)
	default:
		return fmt.Errorf("unexpected topic: %s", msg.Topic)
	}
}

func (c *OrderConsumer) handleOrderAccepted(ctx context.Context, payload []byte) error {
	var event OrderAcceptedEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return kafka.DLQ(fmt.Errorf("decode orders.accepted: %w", err), "decode")
	}
	if err := event.Validate(); err != nil {
		return kafka.DLQ(err, "invalid_event")
	}
	if c.deduper.Seen(event.EventID) {
		return nil
	}
	orderID := strings.TrimSpace(event.OrderID)
	if orderID == "" {
		return kafka.DLQ(fmt.Errorf("order_id required"), "invalid_event")
	}
	if c.orderDeduper.Seen(orderID) {
		return nil
	}

	qty, err := decimal.NewFromString(strings.TrimSpace(event.Quantity))
	if err != nil {
		return kafka.DLQ(fmt.Errorf("invalid quantity: %w", err), "invalid_event")
	}
	price := decimal.Zero
	if strings.TrimSpace(event.Price) != "" {
		price, err = decimal.NewFromString(strings.TrimSpace(event.Price))
		if err != nil {
			return kafka.DLQ(fmt.Errorf("invalid price: %w", err), "invalid_event")
		}
	}

	createdAt := time.Now().UTC()
	if !event.Timestamp.IsZero() {
		if parsed, err := time.Parse(time.RFC3339, event.Timestamp.UTC().Format(time.RFC3339)); err == nil {
			createdAt = parsed
		}
	}

	order := &engine.Order{
		ID:            orderID,
		ClientOrderID: strings.TrimSpace(event.ClientOrderID),
		AccountID:     strings.TrimSpace(event.AccountID),
		Symbol:        strings.TrimSpace(event.Symbol),
		Side:          strings.TrimSpace(event.Side),
		Type:          strings.TrimSpace(event.Type),
		Price:         price,
		Quantity:      qty,
		TimeInForce:   strings.TrimSpace(event.TimeInForce),
		CreatedAt:     createdAt,
	}

	correlationID := strings.TrimSpace(event.CorrelationID)
	if correlationID == "" {
		correlationID = event.EventID
	}
	if _, err := c.engine.ProcessOrder(ctx, order, correlationID); err != nil {
		return err
	}

	c.deduper.Mark(event.EventID)
	c.orderDeduper.Mark(orderID)
	return nil
}

func (c *OrderConsumer) handleOrderCancelled(ctx context.Context, payload []byte) error {
	var event OrderCancelledEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return kafka.DLQ(fmt.Errorf("decode orders.cancelled: %w", err), "decode")
	}
	if err := event.Validate(); err != nil {
		return kafka.DLQ(err, "invalid_event")
	}
	if c.deduper.Seen(event.EventID) {
		return nil
	}

	c.engine.CancelOrder(strings.TrimSpace(event.OrderID), strings.TrimSpace(event.Symbol))
	c.deduper.Mark(event.EventID)
	return nil
}

func (e *OrderAcceptedEvent) Validate() error {
	if err := e.Envelope.Validate(); err != nil {
		return err
	}
	if e.EventType != ordersAcceptedEventType {
		return fmt.Errorf("unexpected event_type: %s", e.EventType)
	}
	if strings.TrimSpace(e.OrderID) == "" {
		return fmt.Errorf("order_id required")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return fmt.Errorf("symbol required")
	}
	if strings.TrimSpace(e.Side) == "" {
		return fmt.Errorf("side required")
	}
	if strings.TrimSpace(e.Type) == "" {
		return fmt.Errorf("type required")
	}
	if strings.TrimSpace(e.Quantity) == "" {
		return fmt.Errorf("quantity required")
	}
	return nil
}

func (e *OrderCancelledEvent) Validate() error {
	if err := e.Envelope.Validate(); err != nil {
		return err
	}
	if e.EventType != ordersCancelledEventType {
		return fmt.Errorf("unexpected event_type: %s", e.EventType)
	}
	if strings.TrimSpace(e.OrderID) == "" {
		return fmt.Errorf("order_id required")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return fmt.Errorf("symbol required")
	}
	return nil
}

type eventDeduper struct {
	mu       sync.Mutex
	maxSize  int
	order    []string
	seenByID map[string]struct{}
}

func newEventDeduper(max int) *eventDeduper {
	if max <= 0 {
		max = 10000
	}
	return &eventDeduper{
		maxSize:  max,
		seenByID: make(map[string]struct{}, max),
	}
}

func (d *eventDeduper) Seen(eventID string) bool {
	if strings.TrimSpace(eventID) == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seenByID[eventID]
	return ok
}

func (d *eventDeduper) Mark(eventID string) {
	if strings.TrimSpace(eventID) == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seenByID[eventID]; ok {
		return
	}
	d.seenByID[eventID] = struct{}{}
	d.order = append(d.order, eventID)
	if len(d.order) > d.maxSize {
		oldest := d.order[0]
		d.order = d.order[1:]
		delete(d.seenByID, oldest)
	}
}

func (d *eventDeduper) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seenByID)
}

func (d *eventDeduper) LastSeen() time.Time {
	return time.Now()
}
