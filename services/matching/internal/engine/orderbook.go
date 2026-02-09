package engine

import (
	"container/heap"
	"container/list"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

const (
	SideBuy  = "buy"
	SideSell = "sell"

	TypeLimit  = "limit"
	TypeMarket = "market"

	TIFGTC = "GTC"
	TIFIOC = "IOC"
	TIFFOK = "FOK"
)

type Order struct {
	ID            string
	ClientOrderID string
	AccountID     string
	Symbol        string
	Side          string
	Type          string
	Price         decimal.Decimal
	Quantity      decimal.Decimal
	Filled        decimal.Decimal
	TimeInForce   string
	CreatedAt     time.Time
}

func (o *Order) Remaining() decimal.Decimal {
	return o.Quantity.Sub(o.Filled)
}

type Trade struct {
	TradeID      string
	Symbol       string
	MakerOrderID string
	TakerOrderID string
	Price        decimal.Decimal
	Quantity     decimal.Decimal
	MakerSide    string
	ExecutedAt   time.Time
}

type OrderBook struct {
	symbol string
	mu     sync.Mutex
	buys   *bookSide
	sells  *bookSide
	orders map[string]*orderRef
}

func NewOrderBook(symbol string) *OrderBook {
	return &OrderBook{
		symbol: symbol,
		buys:   newBookSide(true),
		sells:  newBookSide(false),
		orders: make(map[string]*orderRef),
	}
}

func (ob *OrderBook) Symbol() string {
	return ob.symbol
}

func (ob *OrderBook) Depth(side string) int {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	count := 0
	for _, ref := range ob.orders {
		if ref.side == side {
			count++
		}
	}
	return count
}

func (ob *OrderBook) BestBid() *priceLevel {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	return ob.buys.best()
}

func (ob *OrderBook) BestAsk() *priceLevel {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	return ob.sells.best()
}

func (ob *OrderBook) AddOrder(order *Order) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	return ob.addOrderLocked(order)
}

func (ob *OrderBook) addOrderLocked(order *Order) error {
	if order == nil {
		return fmt.Errorf("order required")
	}
	if strings.TrimSpace(order.ID) == "" {
		return fmt.Errorf("order id required")
	}
	if _, exists := ob.orders[order.ID]; exists {
		return nil
	}
	if order.Type == TypeMarket {
		return nil
	}
	if order.Remaining().LessThanOrEqual(decimal.Zero) {
		return nil
	}

	side := normalizeSide(order.Side)
	if side == SideBuy {
		ref := ob.buys.add(order)
		ob.orders[order.ID] = ref
		return nil
	}
	if side == SideSell {
		ref := ob.sells.add(order)
		ob.orders[order.ID] = ref
		return nil
	}
	return fmt.Errorf("invalid side")
}

func (ob *OrderBook) RemoveOrder(orderID string) bool {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	return ob.removeOrderLocked(orderID)
}

func (ob *OrderBook) removeOrderLocked(orderID string) bool {
	if strings.TrimSpace(orderID) == "" {
		return false
	}
	ref, ok := ob.orders[orderID]
	if !ok {
		return false
	}
	ref.sideBook.remove(ref)
	delete(ob.orders, orderID)
	return true
}

type orderRef struct {
	order    *Order
	element  *list.Element
	level    *priceLevel
	side     string
	sideBook *bookSide
}

type priceLevel struct {
	price  decimal.Decimal
	key    string
	orders *list.List
	index  int
}

type bookSide struct {
	levels map[string]*priceLevel
	heap   priceHeap
}

func newBookSide(isBuy bool) *bookSide {
	side := &bookSide{
		levels: make(map[string]*priceLevel),
		heap:   priceHeap{isMax: isBuy},
	}
	heap.Init(&side.heap)
	return side
}

func (s *bookSide) add(order *Order) *orderRef {
	key := order.Price.String()
	level := s.levels[key]
	if level == nil {
		level = &priceLevel{price: order.Price, key: key, orders: list.New()}
		heap.Push(&s.heap, level)
		s.levels[key] = level
	}
	element := level.orders.PushBack(order)
	return &orderRef{order: order, element: element, level: level, side: normalizeSide(order.Side), sideBook: s}
}

func (s *bookSide) remove(ref *orderRef) {
	if ref == nil || ref.level == nil || ref.element == nil {
		return
	}
	ref.level.orders.Remove(ref.element)
	if ref.level.orders.Len() == 0 {
		heap.Remove(&s.heap, ref.level.index)
		delete(s.levels, ref.level.key)
	}
}

func (s *bookSide) best() *priceLevel {
	if s.heap.Len() == 0 {
		return nil
	}
	return s.heap.levels[0]
}

type priceHeap struct {
	levels []*priceLevel
	isMax  bool
}

func (h priceHeap) Len() int { return len(h.levels) }

func (h priceHeap) Less(i, j int) bool {
	cmp := h.levels[i].price.Cmp(h.levels[j].price)
	if h.isMax {
		return cmp > 0
	}
	return cmp < 0
}

func (h priceHeap) Swap(i, j int) {
	h.levels[i], h.levels[j] = h.levels[j], h.levels[i]
	h.levels[i].index = i
	h.levels[j].index = j
}

func (h *priceHeap) Push(x interface{}) {
	level := x.(*priceLevel)
	level.index = len(h.levels)
	h.levels = append(h.levels, level)
}

func (h *priceHeap) Pop() interface{} {
	old := h.levels
	n := len(old)
	item := old[n-1]
	item.index = -1
	h.levels = old[:n-1]
	return item
}

func normalizeSide(side string) string {
	trimmed := strings.ToLower(strings.TrimSpace(side))
	switch trimmed {
	case SideBuy:
		return SideBuy
	case SideSell:
		return SideSell
	default:
		return ""
	}
}

func normalizeType(orderType string) string {
	trimmed := strings.ToLower(strings.TrimSpace(orderType))
	switch trimmed {
	case TypeLimit:
		return TypeLimit
	case TypeMarket:
		return TypeMarket
	default:
		return ""
	}
}

func normalizeTIF(tif string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(tif))
	switch trimmed {
	case TIFIOC, TIFFOK, TIFGTC:
		return trimmed
	case "":
		return TIFGTC
	default:
		return trimmed
	}
}
