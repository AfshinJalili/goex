package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/services/matching/internal/engine"
	"github.com/IBM/sarama"
)

type fakeEngine struct {
	processed int
	cancelled int
	orders    []string
}

func (f *fakeEngine) ProcessOrder(ctx context.Context, order *engine.Order, correlationID string) ([]engine.Trade, error) {
	f.processed++
	if order != nil {
		f.orders = append(f.orders, order.ID)
	}
	return nil, nil
}

func (f *fakeEngine) CancelOrder(orderID, symbol string) bool {
	f.cancelled++
	return true
}

func TestOrderConsumerHandlesAccepted(t *testing.T) {
	eng := &fakeEngine{}
	consumer := NewOrderConsumer(eng, nil, "orders.accepted", "orders.cancelled")

	env, _ := kafka.NewEnvelopeWithID("evt-1", "orders.accepted", 1, "corr")
	event := OrderAcceptedEvent{
		Envelope:    env,
		OrderID:     "ord-1",
		AccountID:   "acct-1",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Topic: "orders.accepted", Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if eng.processed != 1 {
		t.Fatalf("expected processed 1, got %d", eng.processed)
	}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if eng.processed != 1 {
		t.Fatalf("expected dedupe, got %d", eng.processed)
	}
}

func TestOrderConsumerHandlesCancelled(t *testing.T) {
	eng := &fakeEngine{}
	consumer := NewOrderConsumer(eng, nil, "orders.accepted", "orders.cancelled")

	env, _ := kafka.NewEnvelopeWithID("evt-2", "orders.cancelled", 1, "corr")
	event := OrderCancelledEvent{
		Envelope:  env,
		OrderID:   "ord-2",
		AccountID: "acct-1",
		Symbol:    "BTC-USD",
		Status:    "cancelled",
	}
	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Topic: "orders.cancelled", Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if eng.cancelled != 1 {
		t.Fatalf("expected cancelled 1, got %d", eng.cancelled)
	}
}

func TestOrderConsumerDedupesByOrderID(t *testing.T) {
	eng := &fakeEngine{}
	consumer := NewOrderConsumer(eng, nil, "orders.accepted", "orders.cancelled")

	env1, _ := kafka.NewEnvelopeWithID("evt-10", "orders.accepted", 1, "corr")
	event1 := OrderAcceptedEvent{
		Envelope:    env1,
		OrderID:     "ord-dup",
		AccountID:   "acct-1",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	payload1, _ := json.Marshal(event1)

	env2, _ := kafka.NewEnvelopeWithID("evt-11", "orders.accepted", 1, "corr")
	event2 := event1
	event2.Envelope = env2
	payload2, _ := json.Marshal(event2)

	msg1 := &sarama.ConsumerMessage{Topic: "orders.accepted", Value: payload1}
	msg2 := &sarama.ConsumerMessage{Topic: "orders.accepted", Value: payload2}

	if err := consumer.HandleMessage(context.Background(), msg1); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if err := consumer.HandleMessage(context.Background(), msg2); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if eng.processed != 1 {
		t.Fatalf("expected processed 1, got %d", eng.processed)
	}
	if len(eng.orders) != 1 || eng.orders[0] != "ord-dup" {
		t.Fatalf("expected order ord-dup processed once, got %v", eng.orders)
	}
}

func TestOrderConsumerHonorsConfiguredTopics(t *testing.T) {
	eng := &fakeEngine{}
	consumer := NewOrderConsumer(eng, nil, "orders.accepted.v2", "orders.cancelled.v2")

	env, _ := kafka.NewEnvelopeWithID("evt-20", "orders.accepted", 1, "corr")
	event := OrderAcceptedEvent{
		Envelope:    env,
		OrderID:     "ord-20",
		AccountID:   "acct-1",
		Symbol:      "BTC-USD",
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Topic: "orders.accepted.v2", Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}
	if eng.processed != 1 {
		t.Fatalf("expected processed 1, got %d", eng.processed)
	}
}
