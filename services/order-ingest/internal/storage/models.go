package storage

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	OrderStatusPending   = "pending"
	OrderStatusOpen      = "open"
	OrderStatusFilled    = "filled"
	OrderStatusCancelled = "cancelled"
	OrderStatusRejected  = "rejected"
	OrderStatusExpired   = "expired"
)

type Order struct {
	ID             uuid.UUID
	ClientOrderID  string
	AccountID      uuid.UUID
	Symbol         string
	Side           string
	Type           string
	Price          *decimal.Decimal
	Quantity       decimal.Decimal
	FilledQuantity decimal.Decimal
	Status         string
	TimeInForce    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OrderFilter struct {
	Symbol string
	Status string
	From   *time.Time
	To     *time.Time
	Cursor string
	Limit  int
}

type AuditLog struct {
	ActorID    uuid.UUID
	ActorType  string
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	IP         string
	UserAgent  string
}
