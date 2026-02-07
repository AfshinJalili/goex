package service

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	BalanceLookups     *prometheus.CounterVec
	SettlementsTotal   *prometheus.CounterVec
	SettlementErrors   *prometheus.CounterVec
	SettlementDuration *prometheus.HistogramVec
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
	}

	registry.MustRegister(m.BalanceLookups, m.SettlementsTotal, m.SettlementErrors, m.SettlementDuration)
	return m
}
