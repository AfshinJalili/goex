package service

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	TierLookups     *prometheus.CounterVec
	FeeCalculations *prometheus.CounterVec
	FeeCalcDuration *prometheus.HistogramVec
	CacheRefreshDur prometheus.Histogram
	CacheSize       prometheus.Gauge
	CacheRefreshErr prometheus.Counter
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
		CacheRefreshErr: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "cache_refresh_errors_total",
				Help: "Total cache refresh errors.",
			},
		),
	}

	registry.MustRegister(m.TierLookups, m.FeeCalculations, m.FeeCalcDuration, m.CacheRefreshDur, m.CacheSize, m.CacheRefreshErr)
	return m
}

func (m *Metrics) ObserveRefresh(duration time.Duration) {
	if m == nil {
		return
	}
	m.CacheRefreshDur.Observe(duration.Seconds())
}

func (m *Metrics) SetCacheSize(size int) {
	if m == nil {
		return
	}
	m.CacheSize.Set(float64(size))
}

func (m *Metrics) IncRefreshError() {
	if m == nil {
		return
	}
	m.CacheRefreshErr.Inc()
}
