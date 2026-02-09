package engine

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestMatchOrderFullFill(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	maker := &Order{ID: "maker", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5)}
	if err := book.AddOrder(maker); err != nil {
		t.Fatalf("add maker: %v", err)
	}

	incoming := &Order{ID: "taker", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(2)}
	trades, err := book.MatchOrder(incoming)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].Quantity.String() != "2" {
		t.Fatalf("expected qty 2, got %s", trades[0].Quantity.String())
	}
	if maker.Remaining().String() != "3" {
		t.Fatalf("expected maker remaining 3, got %s", maker.Remaining().String())
	}
}

func TestMatchOrderIOC(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	incoming := &Order{ID: "ioc", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(90), Quantity: decimal.NewFromInt(1), TimeInForce: "IOC"}
	trades, err := book.MatchOrder(incoming)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(trades) != 0 {
		t.Fatalf("expected no trades, got %d", len(trades))
	}
	if book.Depth(SideBuy) != 0 {
		t.Fatalf("expected IOC order not to rest")
	}
}

func TestMatchOrderFOKRejectsPartial(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	maker := &Order{ID: "maker", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if err := book.AddOrder(maker); err != nil {
		t.Fatalf("add maker: %v", err)
	}

	incoming := &Order{ID: "fok", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(2), TimeInForce: "FOK"}
	trades, err := book.MatchOrder(incoming)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(trades) != 0 {
		t.Fatalf("expected no trades, got %d", len(trades))
	}
	if maker.Remaining().String() != "1" {
		t.Fatalf("expected maker unchanged, got %s", maker.Remaining().String())
	}
}

func TestMatchOrderSkipsSelfTrade(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	selfMaker := &Order{ID: "maker-self", AccountID: "acct-1", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	otherMaker := &Order{ID: "maker-other", AccountID: "acct-2", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if err := book.AddOrder(selfMaker); err != nil {
		t.Fatalf("add self maker: %v", err)
	}
	if err := book.AddOrder(otherMaker); err != nil {
		t.Fatalf("add other maker: %v", err)
	}

	incoming := &Order{ID: "taker", AccountID: "acct-1", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	trades, err := book.MatchOrder(incoming)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}
	if trades[0].MakerOrderID != "maker-other" {
		t.Fatalf("expected match against other account, got %s", trades[0].MakerOrderID)
	}
	if selfMaker.Remaining().String() != "1" {
		t.Fatalf("expected self order to remain, got %s", selfMaker.Remaining().String())
	}
}

func TestMatchOrderSelfTradeOnly(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	selfMaker := &Order{ID: "maker-self", AccountID: "acct-1", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if err := book.AddOrder(selfMaker); err != nil {
		t.Fatalf("add self maker: %v", err)
	}

	incoming := &Order{ID: "taker", AccountID: "acct-1", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	trades, err := book.MatchOrder(incoming)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(trades) != 0 {
		t.Fatalf("expected 0 trades, got %d", len(trades))
	}
	if book.Depth(SideSell) != 0 {
		t.Fatalf("expected self sell removed")
	}
	if book.Depth(SideBuy) != 1 {
		t.Fatalf("expected incoming to rest on book")
	}
}

func TestMatchOrderRejectsSelfCrossingLevel(t *testing.T) {
	book := NewOrderBook("BTC-USD")
	selfSell := &Order{ID: "sell-self", AccountID: "acct-1", Symbol: "BTC-USD", Side: "sell", Type: "limit", Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1)}
	if err := book.AddOrder(selfSell); err != nil {
		t.Fatalf("add self sell: %v", err)
	}

	incoming := &Order{ID: "buy-self", AccountID: "acct-1", Symbol: "BTC-USD", Side: "buy", Type: "limit", Price: decimal.NewFromInt(110), Quantity: decimal.NewFromInt(1)}
	trades, err := book.MatchOrder(incoming)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(trades) != 0 {
		t.Fatalf("expected 0 trades, got %d", len(trades))
	}
	if book.Depth(SideSell) != 0 {
		t.Fatalf("expected self sell removed")
	}
	if book.Depth(SideBuy) != 1 {
		t.Fatalf("expected incoming to rest on book")
	}
}
