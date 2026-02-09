package kafka

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

type handlerFunc func(context.Context, *sarama.ConsumerMessage) error

func (h handlerFunc) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	return h(ctx, msg)
}

type stubSession struct {
	ctx     context.Context
	marked  int
	offsets []string
}

func (s *stubSession) Context() context.Context { return s.ctx }
func (s *stubSession) Claims() map[string][]int32 {
	return map[string][]int32{}
}
func (s *stubSession) MemberID() string                                 { return "" }
func (s *stubSession) GenerationID() int32                              { return 0 }
func (s *stubSession) MarkOffset(_ string, _ int32, _ int64, _ string)  {}
func (s *stubSession) ResetOffset(_ string, _ int32, _ int64, _ string) {}
func (s *stubSession) MarkMessage(_ *sarama.ConsumerMessage, _ string) {
	s.marked++
}
func (s *stubSession) Commit() {}

type stubClaim struct {
	msgCh chan *sarama.ConsumerMessage
}

func (c *stubClaim) Topic() string                            { return "orders.accepted" }
func (c *stubClaim) Partition() int32                         { return 0 }
func (c *stubClaim) InitialOffset() int64                     { return 0 }
func (c *stubClaim) HighWaterMarkOffset() int64               { return 0 }
func (c *stubClaim) Messages() <-chan *sarama.ConsumerMessage { return c.msgCh }

func TestConsumerGroupHandlerDLQsOnError(t *testing.T) {
	dlq := &stubPublisher{}
	handler := &consumerGroupHandler{
		handler: handlerFunc(func(_ context.Context, _ *sarama.ConsumerMessage) error {
			return DLQ(errors.New("decode failed"), "decode")
		}),
		logger:       slog.Default(),
		dlqPublisher: dlq,
		dlqTopic:     "dead_letter",
		retryTracker: newRetryTracker(1, time.Minute),
	}

	msgCh := make(chan *sarama.ConsumerMessage, 1)
	msgCh <- &sarama.ConsumerMessage{Topic: "orders.accepted", Partition: 0, Offset: 1, Value: []byte("bad")}
	close(msgCh)

	session := &stubSession{ctx: context.Background()}
	claim := &stubClaim{msgCh: msgCh}

	if err := handler.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("consume claim error: %v", err)
	}
	if session.marked != 1 {
		t.Fatalf("expected message to be marked, got %d", session.marked)
	}
	if len(dlq.calls) != 1 {
		t.Fatalf("expected dlq publish, got %d", len(dlq.calls))
	}
	if dlq.calls[0].topic != "dead_letter" {
		t.Fatalf("expected dlq topic, got %s", dlq.calls[0].topic)
	}
	if _, ok := dlq.calls[0].value.(DLQPayload); !ok {
		t.Fatalf("expected DLQPayload, got %T", dlq.calls[0].value)
	}
}
