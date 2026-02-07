package service

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	OrderSubmissions       *prometheus.CounterVec
	OrderSubmissionLatency *prometheus.HistogramVec
	OrderCancellations     *prometheus.CounterVec
	TradeEventsProcessed   *prometheus.CounterVec
	RiskCheckDuration      *prometheus.HistogramVec
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		OrderSubmissions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "order_submissions_total",
				Help: "Total order submission attempts.",
			},
			[]string{"status"},
		),
		OrderSubmissionLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "order_submission_latency_seconds",
				Help:    "Order submission latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"status"},
		),
		OrderCancellations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "order_cancellations_total",
				Help: "Total order cancellation attempts.",
			},
			[]string{"status"},
		),
		TradeEventsProcessed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "trade_events_processed_total",
				Help: "Total trades.executed events processed.",
			},
			[]string{"status"},
		),
		RiskCheckDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "risk_check_duration_seconds",
				Help:    "Risk pre-trade check latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"result"},
		),
	}

	registry.MustRegister(
		m.OrderSubmissions,
		m.OrderSubmissionLatency,
		m.OrderCancellations,
		m.TradeEventsProcessed,
		m.RiskCheckDuration,
	)
	return m
}
