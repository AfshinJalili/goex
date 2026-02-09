package storage

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type LedgerAccount struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	Asset            string
	BalanceAvailable decimal.Decimal
	BalanceLocked    decimal.Decimal
	UpdatedAt        time.Time
}

type LedgerEntry struct {
	ID              uuid.UUID
	LedgerAccountID uuid.UUID
	AccountID       uuid.UUID
	Asset           string
	EntryType       string
	Amount          decimal.Decimal
	ReferenceType   string
	ReferenceID     uuid.UUID
	CreatedAt       time.Time
}

type BalanceReservation struct {
	ID             uuid.UUID
	OrderID        uuid.UUID
	AccountID      uuid.UUID
	Asset          string
	Amount         decimal.Decimal
	ConsumedAmount decimal.Decimal
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type SettlementRequest struct {
	TradeID        uuid.UUID
	MakerOrderID   uuid.UUID
	TakerOrderID   uuid.UUID
	MakerAccountID uuid.UUID
	TakerAccountID uuid.UUID
	Symbol         string
	Price          decimal.Decimal
	Quantity       decimal.Decimal
	MakerSide      string
	EventID        string
}

type SettlementResult struct {
	EntryIDs         []uuid.UUID
	Entries          []LedgerEntry
	Balances         []LedgerAccount
	AlreadyProcessed bool
	FeeAccountID     uuid.UUID
}
