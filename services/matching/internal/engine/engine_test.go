package engine

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

type testProducer struct {
	topics []string
	values []any
}

func (p *testProducer) PublishJSON(_ context.Context, topic, _ string, value any) (int32, int64, error) {
	p.topics = append(p.topics, topic)
	p.values = append(p.values, value)
	return 0, 0, nil
}

func (p *testProducer) Close() error { return nil }

func TestEngineProcessOrderPublishesTrades(t *testing.T) {
	producer := &testProducer{}
	engine := NewEngine(nil, producer, "trades.executed", nil, nil)

	sell := &Order{ID: "maker", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if _, err := engine.ProcessOrder(context.Background(), sell, "corr-1"); err != nil {
		t.Fatalf("process sell: %v", err)
	}

	buy := &Order{ID: "taker", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	trades, err := engine.ProcessOrder(context.Background(), buy, "corr-2")
	if err != nil {
		t.Fatalf("process buy: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if len(producer.topics) != 1 || producer.topics[0] != "trades.executed" {
		t.Fatalf("expected trades.executed publish")
	}
}

func TestEngineCancelOrder(t *testing.T) {
	engine := NewEngine(nil, nil, "trades.executed", nil, nil)
	order := &Order{ID: "o1", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if _, err := engine.ProcessOrder(context.Background(), order, ""); err != nil {
		t.Fatalf("process order: %v", err)
	}
	if !engine.CancelOrder(order.ID, order.Symbol) {
		t.Fatalf("expected cancel true")
	}
}
