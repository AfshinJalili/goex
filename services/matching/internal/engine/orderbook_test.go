package engine

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestOrderBookBestPrices(t *testing.T) {
	book := NewOrderBook("BTC-USD")

	buy1 := &Order{ID: "b1", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	buy2 := &Order{ID: "b2", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(105), Quantity: decimal.NewFromInt(1)}
	sell1 := &Order{ID: "s1", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(110), Quantity: decimal.NewFromInt(1)}
	sell2 := &Order{ID: "s2", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(108), Quantity: decimal.NewFromInt(1)}

	if err := book.AddOrder(buy1); err != nil {
		t.Fatalf("add buy1: %v", err)
	}
	if err := book.AddOrder(buy2); err != nil {
		t.Fatalf("add buy2: %v", err)
	}
	if err := book.AddOrder(sell1); err != nil {
		t.Fatalf("add sell1: %v", err)
	}
	if err := book.AddOrder(sell2); err != nil {
		t.Fatalf("add sell2: %v", err)
	}

	bestBid := book.BestBid()
	if bestBid == nil || bestBid.price.String() != "105" {
		t.Fatalf("expected best bid 105, got %v", bestBid)
	}

	bestAsk := book.BestAsk()
	if bestAsk == nil || bestAsk.price.String() != "108" {
		t.Fatalf("expected best ask 108, got %v", bestAsk)
	}
}

func TestOrderBookRemove(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	order := &Order{ID: "o1", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if err := book.AddOrder(order); err != nil {
		t.Fatalf("add order: %v", err)
	}
	if !book.RemoveOrder(order.ID) {
		t.Fatalf("expected remove to succeed")
	}
	if book.RemoveOrder(order.ID) {
		t.Fatalf("expected second remove to fail")
	}
}
