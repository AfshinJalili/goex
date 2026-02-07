package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

func (ob *OrderBook) MatchOrder(incoming *Order) ([]Trade, error) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if incoming == nil {
		return nil, fmt.Errorf("order required")
	}
	side := normalizeSide(incoming.Side)
	if side == "" {
		return nil, fmt.Errorf("invalid side")
	}
	orderType := normalizeType(incoming.Type)
	if orderType == "" {
		return nil, fmt.Errorf("invalid order type")
	}
	incoming.TimeInForce = normalizeTIF(incoming.TimeInForce)

	if incoming.Quantity.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("quantity must be positive")
	}
	if orderType == TypeLimit && incoming.Price.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("price must be positive for limit orders")
	}

	if incoming.TimeInForce == TIFFOK {
		if !ob.canFill(incoming) {
			return nil, nil
		}
	}

	opposite := ob.sells
	if side == SideSell {
		opposite = ob.buys
	}

	trades := make([]Trade, 0)
	remaining := incoming.Remaining()

	for remaining.GreaterThan(decimal.Zero) {
		best := opposite.best()
		if best == nil {
			break
		}
		if !priceCrosses(incoming, best.price) {
			break
		}

		makerElem := best.orders.Front()
		if makerElem == nil {
			break
		}
		maker := makerElem.Value.(*Order)
		makerRemaining := maker.Remaining()
		if makerRemaining.LessThanOrEqual(decimal.Zero) {
			ob.removeOrderLocked(maker.ID)
			continue
		}

		matchQty := minDecimal(remaining, makerRemaining)
		maker.Filled = maker.Filled.Add(matchQty)
		incoming.Filled = incoming.Filled.Add(matchQty)
		remaining = incoming.Remaining()

		trade := Trade{
			TradeID:      "",
			Symbol:       incoming.Symbol,
			MakerOrderID: maker.ID,
			TakerOrderID: incoming.ID,
			Price:        best.price,
			Quantity:     matchQty,
			MakerSide:    maker.Side,
			ExecutedAt:   time.Now().UTC(),
		}
		trades = append(trades, trade)

		if maker.Remaining().LessThanOrEqual(decimal.Zero) {
			ob.removeOrderLocked(maker.ID)
		}
	}

	if incoming.Remaining().GreaterThan(decimal.Zero) {
		if incoming.TimeInForce == TIFIOC || incoming.TimeInForce == TIFFOK {
			return trades, nil
		}
		if orderType == TypeLimit {
			if err := ob.addOrderLocked(incoming); err != nil {
				return trades, err
			}
		}
	}

	return trades, nil
}

func (ob *OrderBook) canFill(incoming *Order) bool {
	side := normalizeSide(incoming.Side)
	orderType := normalizeType(incoming.Type)
	if side == "" || orderType == "" {
		return false
	}

	opposite := ob.sells
	if side == SideSell {
		opposite = ob.buys
	}

	levels := make([]*priceLevel, 0, len(opposite.levels))
	for _, level := range opposite.levels {
		levels = append(levels, level)
	}

	sort.Slice(levels, func(i, j int) bool {
		cmp := levels[i].price.Cmp(levels[j].price)
		if side == SideBuy {
			return cmp < 0
		}
		return cmp > 0
	})

	remaining := incoming.Remaining()
	for _, level := range levels {
		if !priceCrosses(incoming, level.price) {
			break
		}
		for e := level.orders.Front(); e != nil && remaining.GreaterThan(decimal.Zero); e = e.Next() {
			maker := e.Value.(*Order)
			makerRemaining := maker.Remaining()
			if makerRemaining.LessThanOrEqual(decimal.Zero) {
				continue
			}
			remaining = remaining.Sub(makerRemaining)
		}
		if remaining.LessThanOrEqual(decimal.Zero) {
			return true
		}
	}

	return remaining.LessThanOrEqual(decimal.Zero)
}

func priceCrosses(incoming *Order, makerPrice decimal.Decimal) bool {
	orderType := normalizeType(incoming.Type)
	if orderType == TypeMarket {
		return true
	}
	side := normalizeSide(incoming.Side)
	if side == SideBuy {
		return makerPrice.Cmp(incoming.Price) <= 0
	}
	if side == SideSell {
		return makerPrice.Cmp(incoming.Price) >= 0
	}
	return false
}

func minDecimal(a, b decimal.Decimal) decimal.Decimal {
	if a.Cmp(b) <= 0 {
		return a
	}
	return b
}

func validateOrderFields(order *Order) error {
	if order == nil {
		return fmt.Errorf("order required")
	}
	if strings.TrimSpace(order.ID) == "" {
		return fmt.Errorf("order id required")
	}
	if strings.TrimSpace(order.Symbol) == "" {
		return fmt.Errorf("symbol required")
	}
	if normalizeSide(order.Side) == "" {
		return fmt.Errorf("invalid side")
	}
	if normalizeType(order.Type) == "" {
		return fmt.Errorf("invalid type")
	}
	if order.Quantity.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("quantity must be positive")
	}
	if normalizeType(order.Type) == TypeLimit && order.Price.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("price must be positive for limit")
	}
	return nil
}
