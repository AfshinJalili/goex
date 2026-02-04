package storage

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.New()
	ts := time.Date(2026, 2, 4, 10, 0, 0, 0, time.UTC)

	cursor := encodeCursor(ts, id)
	if cursor == "" {
		t.Fatalf("expected cursor")
	}
	decodedTS, decodedID, err := decodeCursor(cursor)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !decodedTS.Equal(ts) {
		t.Fatalf("expected ts %v, got %v", ts, decodedTS)
	}
	if decodedID != id {
		t.Fatalf("expected id %v, got %v", id, decodedID)
	}
}

func TestDecodeCursorInvalid(t *testing.T) {
	_, _, err := decodeCursor("not-base64")
	if err == nil {
		t.Fatalf("expected error for invalid cursor")
	}
}
