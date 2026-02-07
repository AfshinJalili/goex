package storage

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type AccountInfo struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Status   string
	KYCLevel string
}

type Market struct {
	ID         uuid.UUID
	Symbol     string
	BaseAsset  string
	QuoteAsset string
	Status     string
}

type BalanceCheck struct {
	Asset      string
	Available  decimal.Decimal
	Required   decimal.Decimal
	Sufficient bool
}
