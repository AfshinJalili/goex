package kafka

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

type stubPublisher struct {
	mu    sync.Mutex
	calls []publishCall
	err   error
}

type publishCall struct {
	topic string
	key   string
	value any
}

func (s *stubPublisher) PublishJSON(_ context.Context, topic, key string, value any) (int32, int64, error) {
	s.mu.Lock()
	s.calls = append(s.calls, publishCall{topic: topic, key: key, value: value})
	s.mu.Unlock()
	if s.err != nil {
		return 0, 0, s.err
	}
	return 0, 0, nil
}

func (s *stubPublisher) Close() error { return nil }

func TestDLQPublisherPublishesOnError(t *testing.T) {
	primary := &stubPublisher{err: errors.New("publish failed")}
	dlq := &stubPublisher{}
	publisher := NewDLQPublisher(primary, dlq, "dead_letter", slog.Default())

	_, _, err := publisher.PublishJSON(context.Background(), "orders.accepted", "key-1", map[string]string{"id": "1"})
	if err == nil {
		t.Fatalf("expected publish error")
	}
	if len(dlq.calls) != 1 {
		t.Fatalf("expected dlq publish, got %d", len(dlq.calls))
	}
	if dlq.calls[0].topic != "dead_letter" {
		t.Fatalf("expected dlq topic, got %s", dlq.calls[0].topic)
	}
	payload, ok := dlq.calls[0].value.(DLQPublishPayload)
	if !ok {
		t.Fatalf("expected DLQPublishPayload, got %T", dlq.calls[0].value)
	}
	if payload.OriginalTopic != "orders.accepted" {
		t.Fatalf("expected original topic to match, got %s", payload.OriginalTopic)
	}
	if payload.Error == "" {
		t.Fatalf("expected error in dlq payload")
	}
}

func TestDLQPublisherSkipsOnSuccess(t *testing.T) {
	primary := &stubPublisher{}
	dlq := &stubPublisher{}
	publisher := NewDLQPublisher(primary, dlq, "dead_letter", slog.Default())

	if _, _, err := publisher.PublishJSON(context.Background(), "orders.accepted", "key-1", map[string]string{"id": "1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dlq.calls) != 0 {
		t.Fatalf("expected no dlq publish, got %d", len(dlq.calls))
	}
}
