package service

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	BalanceLookups     *prometheus.CounterVec
	SettlementsTotal   *prometheus.CounterVec
	SettlementErrors   *prometheus.CounterVec
	SettlementDuration *prometheus.HistogramVec
	FeeCallDuration    *prometheus.HistogramVec
	FeeCallRetries     prometheus.Counter
	FeeCallFailures    *prometheus.CounterVec
	FeeFallbacks       *prometheus.CounterVec
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		BalanceLookups: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ledger_balance_lookups_total",
				Help: "Total balance lookups.",
			},
			[]string{"status"},
		),
		SettlementsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ledger_settlements_total",
				Help: "Total settlements processed.",
			},
			[]string{"status"},
		),
		SettlementErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ledger_settlement_errors_total",
				Help: "Total settlement errors.",
			},
			[]string{"type"},
		),
		SettlementDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ledger_settlement_duration_seconds",
				Help:    "Settlement processing duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method"},
		),
		FeeCallDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ledger_fee_call_duration_seconds",
				Help:    "Fee service call duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"status"},
		),
		FeeCallRetries: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "ledger_fee_call_retries_total",
				Help: "Total fee service call retries.",
			},
		),
		FeeCallFailures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ledger_fee_call_failures_total",
				Help: "Total fee service call failures.",
			},
			[]string{"reason"},
		),
		FeeFallbacks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ledger_fee_call_fallbacks_total",
				Help: "Total fee call fallbacks.",
			},
			[]string{"policy"},
		),
	}

	registry.MustRegister(
		m.BalanceLookups,
		m.SettlementsTotal,
		m.SettlementErrors,
		m.SettlementDuration,
		m.FeeCallDuration,
		m.FeeCallRetries,
		m.FeeCallFailures,
		m.FeeFallbacks,
	)
	return m
}

func (m *Metrics) ObserveFeeCall(status string, duration time.Duration) {
	if m == nil {
		return
	}
	m.FeeCallDuration.WithLabelValues(status).Observe(duration.Seconds())
}

func (m *Metrics) IncFeeRetry() {
	if m == nil {
		return
	}
	m.FeeCallRetries.Inc()
}

func (m *Metrics) IncFeeFailure(reason string) {
	if m == nil {
		return
	}
	m.FeeCallFailures.WithLabelValues(reason).Inc()
}

func (m *Metrics) IncFeeFallback(policy string) {
	if m == nil {
		return
	}
	m.FeeFallbacks.WithLabelValues(policy).Inc()
}
