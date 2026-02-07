package service

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	OrdersProcessed  *prometheus.CounterVec
	TradesExecuted   *prometheus.CounterVec
	MatchingLatency  *prometheus.HistogramVec
	OrderbookDepth   *prometheus.GaugeVec
	OrderbookSpread  *prometheus.GaugeVec
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		OrdersProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "matching_orders_processed_total",
				Help: "Total orders processed by matching engine.",
			},
			[]string{"symbol", "side", "type"},
		),
		TradesExecuted: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "matching_trades_executed_total",
				Help: "Total trades executed by matching engine.",
			},
			[]string{"symbol"},
		),
		MatchingLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "matching_latency_seconds",
				Help:    "Order matching latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"symbol"},
		),
		OrderbookDepth: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "matching_orderbook_depth",
				Help: "Order book depth by symbol and side.",
			},
			[]string{"symbol", "side"},
		),
		OrderbookSpread: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "matching_orderbook_spread",
				Help: "Order book spread by symbol.",
			},
			[]string{"symbol"},
		),
	}

	registry.MustRegister(m.OrdersProcessed, m.TradesExecuted, m.MatchingLatency, m.OrderbookDepth, m.OrderbookSpread)
	return m
}

func (m *Metrics) ObserveOrder(symbol, side, orderType string, duration time.Duration) {
	if m == nil {
		return
	}
	m.OrdersProcessed.WithLabelValues(symbol, side, orderType).Inc()
	m.MatchingLatency.WithLabelValues(symbol).Observe(duration.Seconds())
}

func (m *Metrics) ObserveTrades(symbol string, count int) {
	if m == nil {
		return
	}
	m.TradesExecuted.WithLabelValues(symbol).Add(float64(count))
}

func (m *Metrics) SetOrderbookDepth(symbol, side string, depth float64) {
	if m == nil {
		return
	}
	m.OrderbookDepth.WithLabelValues(symbol, side).Set(depth)
}

func (m *Metrics) SetOrderbookSpread(symbol string, spread float64) {
	if m == nil {
		return
	}
	m.OrderbookSpread.WithLabelValues(symbol).Set(spread)
}
