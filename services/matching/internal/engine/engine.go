package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/google/uuid"
	"log/slog"
)

type SnapshotStore interface {
	LoadOpenOrders(ctx context.Context, symbol string) ([]*Order, error)
}

type Metrics interface {
	ObserveOrder(symbol, side, orderType string, duration time.Duration)
	ObserveTrades(symbol string, count int)
	SetOrderbookDepth(symbol, side string, depth float64)
	SetOrderbookSpread(symbol string, spread float64)
}

type Engine struct {
	mu      sync.RWMutex
	books   map[string]*OrderBook
	store   SnapshotStore
	logger  *slog.Logger
	metrics Metrics
	producer  kafka.Publisher
	tradeTopic string
}

func NewEngine(store SnapshotStore, producer kafka.Publisher, tradeTopic string, logger *slog.Logger, metrics Metrics) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(tradeTopic) == "" {
		tradeTopic = "trades.executed"
	}
	return &Engine{
		books:      make(map[string]*OrderBook),
		store:      store,
		logger:     logger,
		metrics:    metrics,
		producer:   producer,
		tradeTopic: tradeTopic,
	}
}

func (e *Engine) ProcessOrder(ctx context.Context, order *Order, correlationID string) ([]Trade, error) {
	start := time.Now()
	if err := validateOrderFields(order); err != nil {
		return nil, err
	}
	book := e.getOrderBook(order.Symbol)

	trades, err := book.MatchOrder(order)
	if err != nil {
		return nil, err
	}

	if len(trades) > 0 {
		for idx := range trades {
			trades[idx].TradeID = kafka.DeterministicEventID("trades.executed", order.ID, fmt.Sprintf("%d", idx))
			if err := e.publishTrade(ctx, trades[idx], correlationID); err != nil {
				return nil, err
			}
		}
	}

	e.updateMetrics(order, len(trades), time.Since(start))
	return trades, nil
}

func (e *Engine) CancelOrder(orderID, symbol string) bool {
	book := e.getOrderBook(symbol)
	return book.RemoveOrder(orderID)
}

func (e *Engine) LoadSnapshot(ctx context.Context, symbol string) (int, []string, error) {
	if e.store == nil {
		return 0, nil, fmt.Errorf("snapshot store not configured")
	}

	if strings.TrimSpace(symbol) == "" {
		e.mu.Lock()
		e.books = make(map[string]*OrderBook)
		e.mu.Unlock()
	} else {
		e.mu.Lock()
		e.books[symbol] = NewOrderBook(symbol)
		e.mu.Unlock()
	}

	orders, err := e.store.LoadOpenOrders(ctx, symbol)
	if err != nil {
		return 0, nil, err
	}

	loaded := 0
	symbolSet := make(map[string]struct{})

	for _, order := range orders {
		if order == nil {
			continue
		}
		book := e.getOrderBook(order.Symbol)
		if err := book.AddOrder(order); err != nil {
			e.logger.Error("snapshot order load failed", "order_id", order.ID, "error", err)
			continue
		}
		loaded++
		symbolSet[order.Symbol] = struct{}{}
	}

	symbols := make([]string, 0, len(symbolSet))
	for sym := range symbolSet {
		symbols = append(symbols, sym)
	}

	return loaded, symbols, nil
}

func (e *Engine) ActiveSymbols() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.books)
}

func (e *Engine) TotalOrders() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, book := range e.books {
		count += book.Depth(SideBuy)
		count += book.Depth(SideSell)
	}
	return count
}

func (e *Engine) getOrderBook(symbol string) *OrderBook {
	sym := strings.TrimSpace(symbol)
	if sym == "" {
		sym = "UNKNOWN"
	}

	e.mu.RLock()
	book := e.books[sym]
	e.mu.RUnlock()
	if book != nil {
		return book
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	book = e.books[sym]
	if book == nil {
		book = NewOrderBook(sym)
		e.books[sym] = book
	}
	return book
}

func (e *Engine) publishTrade(ctx context.Context, trade Trade, correlationID string) error {
	if e.producer == nil {
		return fmt.Errorf("kafka producer not configured")
	}
	if trade.TradeID == "" {
		trade.TradeID = uuid.NewString()
	}

	env, err := kafka.NewEnvelopeWithID(trade.TradeID, "trades.executed", 1, correlationID)
	if err != nil {
		return err
	}

	payload := TradeExecutedEvent{
		Envelope:     env,
		TradeID:      trade.TradeID,
		Symbol:       trade.Symbol,
		MakerOrderID: trade.MakerOrderID,
		TakerOrderID: trade.TakerOrderID,
		Price:        trade.Price.String(),
		Quantity:     trade.Quantity.String(),
		MakerSide:    trade.MakerSide,
		ExecutedAt:   trade.ExecutedAt.UTC().Format(time.RFC3339),
	}

	_, _, err = e.producer.PublishJSON(ctx, e.tradeTopic, trade.Symbol, payload)
	return err
}

func (e *Engine) updateMetrics(order *Order, trades int, duration time.Duration) {
	if e.metrics == nil || order == nil {
		return
	}
	e.metrics.ObserveOrder(order.Symbol, order.Side, order.Type, duration)
	if trades > 0 {
		e.metrics.ObserveTrades(order.Symbol, trades)
	}

	book := e.getOrderBook(order.Symbol)
	depthBuy := float64(book.Depth(SideBuy))
	depthSell := float64(book.Depth(SideSell))
	e.metrics.SetOrderbookDepth(order.Symbol, SideBuy, depthBuy)
	e.metrics.SetOrderbookDepth(order.Symbol, SideSell, depthSell)

	bestBid := book.BestBid()
	bestAsk := book.BestAsk()
	if bestBid != nil && bestAsk != nil {
		spread := bestAsk.price.Sub(bestBid.price)
		if spread.IsNegative() {
			spread = spread.Abs()
		}
		e.metrics.SetOrderbookSpread(order.Symbol, spread.InexactFloat64())
	}
}
