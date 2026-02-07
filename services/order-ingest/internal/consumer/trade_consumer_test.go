package consumer

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/services/order-ingest/internal/storage"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type fakeStore struct {
	eventID string
	fills   []storage.OrderFill
	already bool
	err     error
}

func (f *fakeStore) ApplyTradeExecution(ctx context.Context, eventID string, fills []storage.OrderFill) (bool, error) {
	f.eventID = eventID
	f.fills = fills
	return f.already, f.err
}

func TestTradeConsumerHandlesEvent(t *testing.T) {
	store := &fakeStore{}
	consumer := NewTradeConsumer(store, nil, nil)

	event := TradeExecutedEvent{
		Envelope: kafka.Envelope{
			EventID:      "evt_1",
			EventType:    tradesExecutedEventType,
			EventVersion: 1,
			Timestamp:    time.Now().UTC(),
		},
		TradeID:      uuid.NewString(),
		Symbol:       "BTC-USD",
		MakerOrderID: uuid.NewString(),
		TakerOrderID: uuid.NewString(),
		Price:        "100",
		Quantity:     "2",
		MakerSide:    "buy",
		ExecutedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if store.eventID != event.EventID {
		t.Fatalf("expected eventID %s, got %s", event.EventID, store.eventID)
	}
	if len(store.fills) != 2 {
		t.Fatalf("expected 2 fills, got %d", len(store.fills))
	}
	if !store.fills[0].Quantity.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("expected quantity 2")
	}
}

func TestTradeConsumerAlreadyProcessed(t *testing.T) {
	store := &fakeStore{already: true}
	consumer := NewTradeConsumer(store, nil, nil)

	event := TradeExecutedEvent{
		Envelope: kafka.Envelope{
			EventID:      "evt_2",
			EventType:    tradesExecutedEventType,
			EventVersion: 1,
			Timestamp:    time.Now().UTC(),
		},
		TradeID:      uuid.NewString(),
		Symbol:       "BTC-USD",
		MakerOrderID: uuid.NewString(),
		TakerOrderID: uuid.NewString(),
		Price:        "100",
		Quantity:     "1",
		MakerSide:    "sell",
		ExecutedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}
