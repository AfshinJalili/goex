package service

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	PreTradeChecks       *prometheus.CounterVec
	PreTradeCheckLatency *prometheus.HistogramVec
	CacheHits            *prometheus.CounterVec
	CacheRefreshDur      prometheus.Histogram
	CacheSize            prometheus.Gauge
}

func NewMetrics(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		PreTradeChecks: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "risk_pre_trade_checks_total",
				Help: "Total pre-trade checks.",
			},
			[]string{"result", "reason"},
		),
		PreTradeCheckLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "risk_pre_trade_check_duration_seconds",
				Help:    "Pre-trade check duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"result"},
		),
		CacheHits: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "risk_cache_hits_total",
				Help: "Market cache hit/miss count.",
			},
			[]string{"status"},
		),
		CacheRefreshDur: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "risk_cache_refresh_duration_seconds",
				Help:    "Market cache refresh duration in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		),
		CacheSize: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "risk_cache_size",
				Help: "Number of markets in cache.",
			},
		),
	}

	registry.MustRegister(m.PreTradeChecks, m.PreTradeCheckLatency, m.CacheHits, m.CacheRefreshDur, m.CacheSize)
	return m
}
