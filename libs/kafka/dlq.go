package kafka

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
)

type DLQError struct {
	Err    error
	Reason string
}

func (e *DLQError) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Reason, e.Err)
}

func (e *DLQError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func DLQ(err error, reason string) error {
	if err == nil {
		return nil
	}
	return &DLQError{Err: err, Reason: reason}
}

type DLQPayload struct {
	OriginalTopic string    `json:"original_topic"`
	Partition     int32     `json:"partition"`
	Offset        int64     `json:"offset"`
	Key           string    `json:"key,omitempty"`
	Error         string    `json:"error"`
	Reason        string    `json:"reason,omitempty"`
	Attempts      int       `json:"attempts,omitempty"`
	Payload       string    `json:"payload_base64"`
	Timestamp     time.Time `json:"timestamp"`
}

func BuildDLQPayload(msg *sarama.ConsumerMessage, err *DLQError, attempts int) DLQPayload {
	var key string
	if msg != nil && len(msg.Key) > 0 {
		key = string(msg.Key)
	}
	payload := ""
	if msg != nil && len(msg.Value) > 0 {
		payload = base64.StdEncoding.EncodeToString(msg.Value)
	}
	errMsg := ""
	reason := ""
	if err != nil {
		if err.Err != nil {
			errMsg = err.Err.Error()
		} else {
			errMsg = err.Error()
		}
		reason = err.Reason
	}
	return DLQPayload{
		OriginalTopic: msg.Topic,
		Partition:     msg.Partition,
		Offset:        msg.Offset,
		Key:           key,
		Error:         errMsg,
		Reason:        reason,
		Attempts:      attempts,
		Payload:       payload,
		Timestamp:     time.Now().UTC(),
	}
}

type DLQPublishPayload struct {
	OriginalTopic string    `json:"original_topic"`
	Key           string    `json:"key,omitempty"`
	Error         string    `json:"error"`
	Reason        string    `json:"reason,omitempty"`
	Attempts      int       `json:"attempts,omitempty"`
	Payload       string    `json:"payload_base64"`
	Timestamp     time.Time `json:"timestamp"`
}

func BuildPublishDLQPayload(topic, key string, value any, err error, reason string, attempts int) DLQPublishPayload {
	payload := ""
	if value != nil {
		if raw, marshalErr := json.Marshal(value); marshalErr == nil {
			payload = base64.StdEncoding.EncodeToString(raw)
		} else {
			payload = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%v", value)))
		}
	}
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	return DLQPublishPayload{
		OriginalTopic: topic,
		Key:           key,
		Error:         errMsg,
		Reason:        reason,
		Attempts:      attempts,
		Payload:       payload,
		Timestamp:     time.Now().UTC(),
	}
}
