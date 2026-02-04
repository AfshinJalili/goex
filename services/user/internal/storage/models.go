package storage

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID         uuid.UUID
	Email      string
	Status     string
	KYCLevel   string
	MFAEnabled bool
	CreatedAt  time.Time
}

type Account struct {
	ID        uuid.UUID
	Type      string
	Status    string
	CreatedAt time.Time
}

type Balance struct {
	AccountID uuid.UUID
	Asset     string
	Available string
	Locked    string
	UpdatedAt time.Time
}

// AuditLog captures a read action.
type AuditLog struct {
	ActorID    uuid.UUID
	ActorType  string
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	IP         string
	UserAgent  string
}
