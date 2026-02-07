package engine

import "github.com/AfshinJalili/goex/libs/kafka"

type TradeExecutedEvent struct {
	kafka.Envelope
	TradeID      string `json:"trade_id"`
	Symbol       string `json:"symbol"`
	MakerOrderID string `json:"maker_order_id"`
	TakerOrderID string `json:"taker_order_id"`
	Price        string `json:"price"`
	Quantity     string `json:"quantity"`
	MakerSide    string `json:"maker_side"`
	ExecutedAt   string `json:"executed_at"`
}
