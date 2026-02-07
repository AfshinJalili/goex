package service

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	TierLookups     *prometheus.CounterVec
	FeeCalculations *prometheus.CounterVec
	FeeCalcDuration *prometheus.HistogramVec
	CacheRefreshDur prometheus.Histogram
	CacheSize       prometheus.Gauge
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		TierLookups: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "fee_tier_lookups_total",
				Help: "Total fee tier lookups.",
			},
			[]string{"tier_name", "status"},
		),
		FeeCalculations: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "fee_calculations_total",
				Help: "Total fee calculations.",
			},
			[]string{"order_type"},
		),
		FeeCalcDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "fee_calculation_duration_seconds",
				Help:    "Fee calculation duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method"},
		),
		CacheRefreshDur: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "cache_refresh_duration_seconds",
				Help:    "Cache refresh duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		),
		CacheSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "cache_size",
				Help: "Number of fee tiers cached.",
			},
		),
	}

	registry.MustRegister(m.TierLookups, m.FeeCalculations, m.FeeCalcDuration, m.CacheRefreshDur, m.CacheSize)
	return m
}
