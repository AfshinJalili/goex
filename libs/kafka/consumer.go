package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"log/slog"
)

type MessageHandler interface {
	HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error
}

type Consumer struct {
	group  sarama.ConsumerGroup
	logger *slog.Logger
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
		group:  group,
		logger: logger,
	}, nil
}

func (c *Consumer) Consume(ctx context.Context, topics []string, handler MessageHandler) error {
	if handler == nil {
		return fmt.Errorf("message handler required")
	}

	cgHandler := &consumerGroupHandler{
		handler: handler,
		logger:  c.logger,
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

func (c *Consumer) Close() error {
	if c.group == nil {
		return nil
	}
	return c.group.Close()
}

type consumerGroupHandler struct {
	handler MessageHandler
	logger  *slog.Logger
}

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.handler.HandleMessage(session.Context(), msg); err != nil {
			h.logger.Error("kafka message handler error", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset, "error", err)
			continue
		}
		session.MarkMessage(msg, "")
	}
	return nil
}
