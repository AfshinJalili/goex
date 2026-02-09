package kafka

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"log/slog"
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

type Consumer struct {
	group          sarama.ConsumerGroup
	logger         *slog.Logger
	dlqPublisher   Publisher
	dlqTopic       string
	dlqMaxAttempts int
	dlqAttemptTTL  time.Duration
}

func NewConsumer(brokers []string, groupID string, logger *slog.Logger) (*Consumer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers required")
	}
	if groupID == "" {
		return nil, fmt.Errorf("kafka consumer group required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_7_0_0
	cfg.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRange
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest
	cfg.Consumer.Group.Session.Timeout = 30 * time.Second
	cfg.Consumer.Group.Heartbeat.Interval = 3 * time.Second
	cfg.Consumer.Return.Errors = true

	group, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka consumer group: %w", err)
	}

	return &Consumer{
		group:          group,
		logger:         logger,
		dlqMaxAttempts: 3,
		dlqAttemptTTL:  10 * time.Minute,
	}, nil
}

func (c *Consumer) Consume(ctx context.Context, topics []string, handler MessageHandler) error {
	if handler == nil {
		return fmt.Errorf("message handler required")
	}

	cgHandler := &consumerGroupHandler{
		handler:      handler,
		logger:       c.logger,
		dlqPublisher: c.dlqPublisher,
		dlqTopic:     c.dlqTopic,
		retryTracker: newRetryTracker(c.dlqMaxAttempts, c.dlqAttemptTTL),
	}

	for {
		if err := c.group.Consume(ctx, topics, cgHandler); err != nil {
			c.logger.Error("kafka consume error", "error", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(2 * time.Second)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (c *Consumer) WithDLQ(publisher Publisher, topic string) *Consumer {
	if c == nil {
		return c
	}
	if strings.TrimSpace(topic) == "" {
		return c
	}
	c.dlqPublisher = publisher
	c.dlqTopic = topic
	return c
}

func (c *Consumer) WithDLQRetry(maxAttempts int, ttl time.Duration) *Consumer {
	if c == nil {
		return c
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	c.dlqMaxAttempts = maxAttempts
	c.dlqAttemptTTL = ttl
	return c
}

func (c *Consumer) Close() error {
	if c.group == nil {
		return nil
	}
	return c.group.Close()
}

type consumerGroupHandler struct {
	handler      MessageHandler
	logger       *slog.Logger
	dlqPublisher Publisher
	dlqTopic     string
	retryTracker *retryTracker
}

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		key := retryKey(msg)
		if err := h.handler.HandleMessage(session.Context(), msg); err != nil {
			h.logger.Error("kafka message handler error", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
			if h.dlqPublisher != nil && strings.TrimSpace(h.dlqTopic) != "" && msg != nil {
				attempts := 1
				if h.retryTracker != nil {
					attempts = h.retryTracker.Bump(key)
				}
				if shouldSendToDLQ(err, attempts, h.retryTracker.maxAttempts) {
					dlqErr := toDLQError(err, attempts, h.retryTracker.maxAttempts)
					payload := BuildDLQPayload(msg, dlqErr, attempts)
					if _, _, pubErr := h.dlqPublisher.PublishJSON(session.Context(), h.dlqTopic, payload.Key, payload); pubErr == nil {
						session.MarkMessage(msg, "")
						if h.retryTracker != nil {
							h.retryTracker.Reset(key)
						}
						continue
					}
				}
			}
			continue
		}
		session.MarkMessage(msg, "")
		if h.retryTracker != nil {
			h.retryTracker.Reset(key)
		}
	}
	return nil
}

type retryTracker struct {
	mu          sync.Mutex
	attempts    map[string]*retryEntry
	maxAttempts int
	ttl         time.Duration
	lastSweep   time.Time
}

type retryEntry struct {
	count int
	last  time.Time
}

func newRetryTracker(maxAttempts int, ttl time.Duration) *retryTracker {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &retryTracker{
		attempts:    make(map[string]*retryEntry),
		maxAttempts: maxAttempts,
		ttl:         ttl,
		lastSweep:   time.Now(),
	}
}

func (r *retryTracker) Bump(key string) int {
	if r == nil || key == "" {
		return 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.sweepLocked(now)
	entry, ok := r.attempts[key]
	if !ok || now.Sub(entry.last) > r.ttl {
		r.attempts[key] = &retryEntry{count: 1, last: now}
		return 1
	}
	entry.count++
	entry.last = now
	return entry.count
}

func (r *retryTracker) Reset(key string) {
	if r == nil || key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.attempts, key)
}

func (r *retryTracker) sweepLocked(now time.Time) {
	if r.ttl <= 0 {
		return
	}
	if now.Sub(r.lastSweep) < r.ttl {
		return
	}
	for key, entry := range r.attempts {
		if now.Sub(entry.last) > r.ttl {
			delete(r.attempts, key)
		}
	}
	r.lastSweep = now
}

func retryKey(msg *sarama.ConsumerMessage) string {
	if msg == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d", msg.Topic, msg.Partition, msg.Offset)
}

func shouldSendToDLQ(err error, attempts, maxAttempts int) bool {
	if err == nil {
		return false
	}
	var dlqErr *DLQError
	if errors.As(err, &dlqErr) {
		return true
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return attempts >= maxAttempts
}

func toDLQError(err error, attempts, maxAttempts int) *DLQError {
	if err == nil {
		return &DLQError{Err: fmt.Errorf("unknown error"), Reason: "retry_exhausted"}
	}
	var dlqErr *DLQError
	if errors.As(err, &dlqErr) {
		return dlqErr
	}
	reason := "retry_exhausted"
	if attempts < maxAttempts {
		reason = "retry_threshold"
	}
	return &DLQError{Err: err, Reason: reason}
}
