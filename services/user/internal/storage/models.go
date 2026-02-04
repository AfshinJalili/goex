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

type APIKey struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Prefix      string
	Scopes      []string
	IPWhitelist []string
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
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
