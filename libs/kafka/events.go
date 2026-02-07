package kafka

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Envelope struct {
	EventID       string    `json:"event_id"`
	EventType     string    `json:"event_type"`
	EventVersion  int       `json:"event_version"`
	Timestamp     time.Time `json:"timestamp"`
	CorrelationID string    `json:"correlation_id,omitempty"`
}

func NewEnvelope(eventType string, version int, correlationID string) (Envelope, error) {
	if eventType == "" {
		return Envelope{}, fmt.Errorf("event_type is required")
	}
	if version <= 0 {
		return Envelope{}, fmt.Errorf("event_version must be positive")
	}

	return Envelope{
		EventID:       uuid.NewString(),
		EventType:     eventType,
		EventVersion:  version,
		Timestamp:     time.Now().UTC(),
		CorrelationID: correlationID,
	}, nil
}

func NewEnvelopeWithID(eventID, eventType string, version int, correlationID string) (Envelope, error) {
	if eventID == "" {
		return Envelope{}, fmt.Errorf("event_id is required")
	}
	if eventType == "" {
		return Envelope{}, fmt.Errorf("event_type is required")
	}
	if version <= 0 {
		return Envelope{}, fmt.Errorf("event_version must be positive")
	}

	return Envelope{
		EventID:       eventID,
		EventType:     eventType,
		EventVersion:  version,
		Timestamp:     time.Now().UTC(),
		CorrelationID: correlationID,
	}, nil
}

func DeterministicEventID(parts ...string) string {
	joined := strings.Join(parts, "|")
	if joined == "" {
		return uuid.Nil.String()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(joined)).String()
}

func (e Envelope) Validate() error {
	if e.EventID == "" {
		return fmt.Errorf("event_id is required")
	}
	if e.EventType == "" {
		return fmt.Errorf("event_type is required")
	}
	if e.EventVersion <= 0 {
		return fmt.Errorf("event_version must be positive")
	}
	if e.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	return nil
}
