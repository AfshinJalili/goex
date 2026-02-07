package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/services/ledger/internal/storage"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"log/slog"
)

type fakeOrderLookup struct {
	accounts map[uuid.UUID]uuid.UUID
	entries  map[uuid.UUID][]storage.LedgerEntry
	balances []storage.LedgerAccount
}

func (f *fakeOrderLookup) GetOrderAccountID(ctx context.Context, orderID uuid.UUID) (uuid.UUID, error) {
	if accountID, ok := f.accounts[orderID]; ok {
		return accountID, nil
	}
	return uuid.Nil, fmt.Errorf("%w: %s", storage.ErrOrderNotFound, orderID)
}

func (f *fakeOrderLookup) GetEntriesByReference(ctx context.Context, referenceID uuid.UUID) ([]storage.LedgerEntry, error) {
	if entries, ok := f.entries[referenceID]; ok {
		return entries, nil
	}
	return nil, nil
}

func (f *fakeOrderLookup) GetBalancesByAccountAssets(ctx context.Context, assets []storage.LedgerAccount) ([]storage.LedgerAccount, error) {
	return f.balances, nil
}

type fakeLedger struct {
	result    *storage.SettlementResult
	err       error
	lastEvent string
}

func (f *fakeLedger) ApplySettlementInternal(ctx context.Context, req *ledgerpb.ApplySettlementRequest, eventID string) (*storage.SettlementResult, error) {
	f.lastEvent = eventID
	return f.result, f.err
}

type published struct {
	topic string
	key   string
	value any
}

type fakeProducer struct {
	records []published
}

func (f *fakeProducer) PublishJSON(ctx context.Context, topic, key string, value any) (int32, int64, error) {
	f.records = append(f.records, published{topic: topic, key: key, value: value})
	return 0, int64(len(f.records)), nil
}

func (f *fakeProducer) Close() error { return nil }

func TestTradeConsumerValidEvent(t *testing.T) {
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()
	makerAccountID := uuid.New()
	takerAccountID := uuid.New()
	feeAccountID := uuid.New()

	orderLookup := &fakeOrderLookup{
		accounts: map[uuid.UUID]uuid.UUID{
			makerOrderID: makerAccountID,
			takerOrderID: takerAccountID,
		},
	}

	entries := []storage.LedgerEntry{
		{
			ID:          uuid.New(),
			AccountID:   makerAccountID,
			Asset:       "USD",
			EntryType:   "debit",
			Amount:      decimal.NewFromInt(100),
			ReferenceID: uuid.New(),
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          uuid.New(),
			AccountID:   takerAccountID,
			Asset:       "BTC",
			EntryType:   "credit",
			Amount:      decimal.NewFromInt(1),
			ReferenceID: uuid.New(),
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          uuid.New(),
			AccountID:   feeAccountID,
			Asset:       "USD",
			EntryType:   "credit",
			Amount:      decimal.NewFromInt(1),
			ReferenceID: uuid.New(),
			CreatedAt:   time.Now().UTC(),
		},
	}

	balances := []storage.LedgerAccount{
		{AccountID: makerAccountID, Asset: "USD", BalanceAvailable: decimal.NewFromInt(900), BalanceLocked: decimal.Zero, UpdatedAt: time.Now().UTC()},
		{AccountID: takerAccountID, Asset: "BTC", BalanceAvailable: decimal.NewFromInt(11), BalanceLocked: decimal.Zero, UpdatedAt: time.Now().UTC()},
		{AccountID: feeAccountID, Asset: "USD", BalanceAvailable: decimal.NewFromInt(1), BalanceLocked: decimal.Zero, UpdatedAt: time.Now().UTC()},
	}

	ledger := &fakeLedger{
		result: &storage.SettlementResult{Entries: entries, Balances: balances, FeeAccountID: feeAccountID},
	}

	producer := &fakeProducer{}
	consumer := NewTradeConsumer(orderLookup, ledger, producer, slog.Default())

	env, err := kafka.NewEnvelope(tradesExecutedEventType, 1, "corr-1")
	if err != nil {
		t.Fatalf("new envelope: %v", err)
	}

	event := TradeExecutedEvent{
		Envelope:     env,
		TradeID:      uuid.New().String(),
		Symbol:       "BTC-USD",
		MakerOrderID: makerOrderID.String(),
		TakerOrderID: takerOrderID.String(),
		Price:        "100",
		Quantity:     "1",
		MakerSide:    "buy",
		ExecutedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	if len(producer.records) != 6 {
		t.Fatalf("expected 6 published events, got %d", len(producer.records))
	}
}

func TestTradeConsumerDuplicateEvent(t *testing.T) {
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()
	makerAccountID := uuid.New()
	takerAccountID := uuid.New()
	feeAccountID := uuid.New()
	tradeID := uuid.New()

	orderLookup := &fakeOrderLookup{
		accounts: map[uuid.UUID]uuid.UUID{
			makerOrderID: makerAccountID,
			takerOrderID: takerAccountID,
		},
		entries: map[uuid.UUID][]storage.LedgerEntry{
			tradeID: {
				{
					ID:          uuid.New(),
					AccountID:   makerAccountID,
					Asset:       "USD",
					EntryType:   "debit",
					Amount:      decimal.NewFromInt(100),
					ReferenceID: tradeID,
					CreatedAt:   time.Now().UTC(),
				},
				{
					ID:          uuid.New(),
					AccountID:   takerAccountID,
					Asset:       "BTC",
					EntryType:   "credit",
					Amount:      decimal.NewFromInt(1),
					ReferenceID: tradeID,
					CreatedAt:   time.Now().UTC(),
				},
				{
					ID:          uuid.New(),
					AccountID:   feeAccountID,
					Asset:       "USD",
					EntryType:   "credit",
					Amount:      decimal.NewFromInt(1),
					ReferenceID: tradeID,
					CreatedAt:   time.Now().UTC(),
				},
			},
		},
		balances: []storage.LedgerAccount{
			{AccountID: makerAccountID, Asset: "USD", BalanceAvailable: decimal.NewFromInt(900), BalanceLocked: decimal.Zero, UpdatedAt: time.Now().UTC()},
			{AccountID: takerAccountID, Asset: "BTC", BalanceAvailable: decimal.NewFromInt(11), BalanceLocked: decimal.Zero, UpdatedAt: time.Now().UTC()},
			{AccountID: feeAccountID, Asset: "USD", BalanceAvailable: decimal.NewFromInt(1), BalanceLocked: decimal.Zero, UpdatedAt: time.Now().UTC()},
		},
	}
	ledger := &fakeLedger{
		result: &storage.SettlementResult{AlreadyProcessed: true},
	}
	producer := &fakeProducer{}
	consumer := NewTradeConsumer(orderLookup, ledger, producer, slog.Default())

	env, _ := kafka.NewEnvelope(tradesExecutedEventType, 1, "")
	event := TradeExecutedEvent{
		Envelope:     env,
		TradeID:      tradeID.String(),
		Symbol:       "BTC-USD",
		MakerOrderID: makerOrderID.String(),
		TakerOrderID: takerOrderID.String(),
		Price:        "100",
		Quantity:     "1",
		MakerSide:    "buy",
	}
	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("expected no error for duplicate, got %v", err)
	}
	if len(producer.records) != 6 {
		t.Fatalf("expected 6 publishes for duplicate event, got %d", len(producer.records))
	}

	secondProducer := &fakeProducer{}
	consumer = NewTradeConsumer(orderLookup, ledger, secondProducer, slog.Default())
	if err := consumer.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("expected no error for duplicate, got %v", err)
	}

	firstIDs := extractEventIDs(producer.records)
	secondIDs := extractEventIDs(secondProducer.records)
	if len(firstIDs) != len(secondIDs) {
		t.Fatalf("event id count mismatch")
	}
	for key, id := range firstIDs {
		if secondIDs[key] != id {
			t.Fatalf("expected deterministic event id for %s", key)
		}
	}
}

func TestTradeConsumerInvalidEvent(t *testing.T) {
	consumer := NewTradeConsumer(&fakeOrderLookup{accounts: map[uuid.UUID]uuid.UUID{}}, &fakeLedger{}, &fakeProducer{}, slog.Default())
	msg := &sarama.ConsumerMessage{Value: []byte(`{"event_id":"1","event_type":"trades.executed","event_version":1,"timestamp":"2026-01-01T00:00:00Z"}`)}
	if err := consumer.HandleMessage(context.Background(), msg); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestTradeConsumerOrderNotFoundFails(t *testing.T) {
	makerOrderID := uuid.New()
	takerOrderID := uuid.New()

	orderLookup := &fakeOrderLookup{
		accounts: map[uuid.UUID]uuid.UUID{
			takerOrderID: uuid.New(),
		},
	}

	consumer := NewTradeConsumer(orderLookup, &fakeLedger{}, &fakeProducer{}, slog.Default())

	env, _ := kafka.NewEnvelope(tradesExecutedEventType, 1, "")
	event := TradeExecutedEvent{
		Envelope:     env,
		TradeID:      uuid.New().String(),
		Symbol:       "BTC-USD",
		MakerOrderID: makerOrderID.String(),
		TakerOrderID: takerOrderID.String(),
		Price:        "100",
		Quantity:     "1",
		MakerSide:    "buy",
	}
	payload, _ := json.Marshal(event)
	msg := &sarama.ConsumerMessage{Value: payload}

	if err := consumer.HandleMessage(context.Background(), msg); err == nil {
		t.Fatalf("expected error for missing order")
	}
}

func extractEventIDs(records []published) map[string]string {
	ids := make(map[string]string)
	for _, record := range records {
		switch payload := record.value.(type) {
		case LedgerEntriesEvent:
			ids[record.topic+"|"+record.key] = payload.EventID
		case BalancesUpdatedEvent:
			ids[record.topic+"|"+record.key] = payload.EventID
		}
	}
	return ids
}
