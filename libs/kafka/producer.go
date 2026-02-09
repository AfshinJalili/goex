package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
)

type ProducerMetrics struct {
	PublishTotal   *prometheus.CounterVec
	PublishLatency prometheus.Histogram
}

func NewProducerMetrics(registry *prometheus.Registry) *ProducerMetrics {
	m := &ProducerMetrics{
		PublishTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kafka_publish_total",
				Help: "Total Kafka publish attempts.",
			},
			[]string{"topic", "status"},
		),
		PublishLatency: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "kafka_publish_latency_seconds",
				Help:    "Kafka publish latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
		),
	}

	registry.MustRegister(m.PublishTotal, m.PublishLatency)
	return m
}

type Publisher interface {
	PublishJSON(ctx context.Context, topic, key string, value any) (int32, int64, error)
	Close() error
}

type DLQPublisher struct {
	primary  Publisher
	dlq      Publisher
	dlqTopic string
	logger   *slog.Logger
}

func NewDLQPublisher(primary Publisher, dlq Publisher, dlqTopic string, logger *slog.Logger) *DLQPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &DLQPublisher{
		primary:  primary,
		dlq:      dlq,
		dlqTopic: dlqTopic,
		logger:   logger,
	}
}

func (p *DLQPublisher) PublishJSON(ctx context.Context, topic, key string, value any) (int32, int64, error) {
	if p == nil || p.primary == nil {
		return 0, 0, fmt.Errorf("kafka producer not configured")
	}
	partition, offset, err := p.primary.PublishJSON(ctx, topic, key, value)
	if err == nil {
		return partition, offset, nil
	}
	if p.dlq == nil || p.dlqTopic == "" {
		return partition, offset, err
	}
	payload := BuildPublishDLQPayload(topic, key, value, err, "publish_failed", 1)
	if _, _, dlqErr := p.dlq.PublishJSON(ctx, p.dlqTopic, key, payload); dlqErr != nil {
		p.logger.Error("publish dlq failed", "topic", p.dlqTopic, "error", dlqErr)
	}
	return partition, offset, err
}

func (p *DLQPublisher) Close() error {
	if p == nil || p.primary == nil {
		return nil
	}
	return p.primary.Close()
}

type SyncProducer struct {
	producer sarama.SyncProducer
	logger   *slog.Logger
	metrics  *ProducerMetrics
}

func NewSyncProducer(brokers []string, logger *slog.Logger, metrics *ProducerMetrics) (*SyncProducer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_7_0_0
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Return.Successes = true
	cfg.Producer.Return.Errors = true
	cfg.Producer.Idempotent = true
	cfg.Net.MaxOpenRequests = 1
	cfg.Producer.Retry.Max = 5
	cfg.Producer.Retry.Backoff = 250 * time.Millisecond

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}

	return &SyncProducer{
		producer: producer,
		logger:   logger,
		metrics:  metrics,
	}, nil
}

func (p *SyncProducer) PublishJSON(ctx context.Context, topic, key string, value any) (int32, int64, error) {
	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	default:
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal kafka payload: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(payload),
	}

	start := time.Now()
	partition, offset, err := p.producer.SendMessage(msg)
	if p.metrics != nil {
		status := "success"
		if err != nil {
			status = "error"
		}
		p.metrics.PublishTotal.WithLabelValues(topic, status).Inc()
		p.metrics.PublishLatency.Observe(time.Since(start).Seconds())
	}
	if err != nil {
		p.logger.Error("kafka publish failed", "topic", topic, "error", err)
		return 0, 0, fmt.Errorf("kafka publish failed: %w", err)
	}

	return partition, offset, nil
}

func (p *SyncProducer) Close() error {
	if p.producer == nil {
		return nil
	}
	return p.producer.Close()
}
