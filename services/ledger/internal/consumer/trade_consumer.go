package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AfshinJalili/goex/libs/kafka"
	"github.com/AfshinJalili/goex/services/ledger/internal/storage"
	ledgerpb "github.com/AfshinJalili/goex/services/ledger/proto/ledger/v1"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"log/slog"
)

const (
	tradesExecutedEventType  = "trades.executed"
	ledgerEntriesEventType   = "ledger.entries"
	balancesUpdatedEventType = "balances.updated"
)

type TradeExecutedEvent struct {
	kafka.Envelope
	TradeID      string `json:"trade_id"`
	Symbol       string `json:"symbol"`
	MakerOrderID string `json:"maker_order_id"`
	TakerOrderID string `json:"taker_order_id"`
	Price        string `json:"price"`
	Quantity     string `json:"quantity"`
	MakerSide    string `json:"maker_side"`
	ExecutedAt   string `json:"executed_at"`
}

type LedgerEntryEvent struct {
	EntryID     string `json:"entry_id"`
	Asset       string `json:"asset"`
	EntryType   string `json:"entry_type"`
	Amount      string `json:"amount"`
	ReferenceID string `json:"reference_id"`
	CreatedAt   string `json:"created_at"`
}

type BalanceUpdate struct {
	Asset     string `json:"asset"`
	Available string `json:"available"`
	Locked    string `json:"locked"`
	UpdatedAt string `json:"updated_at"`
}

type LedgerEntriesEvent struct {
	kafka.Envelope
	TradeID   string             `json:"trade_id"`
	AccountID string             `json:"account_id"`
	Entries   []LedgerEntryEvent `json:"entries"`
}

type BalancesUpdatedEvent struct {
	kafka.Envelope
	TradeID   string          `json:"trade_id"`
	AccountID string          `json:"account_id"`
	Balances  []BalanceUpdate `json:"balances"`
}

type OrderLookup interface {
	GetOrderAccountID(ctx context.Context, orderID uuid.UUID) (uuid.UUID, error)
	GetEntriesByReference(ctx context.Context, referenceID uuid.UUID) ([]storage.LedgerEntry, error)
	GetBalancesByAccountAssets(ctx context.Context, assets []storage.LedgerAccount) ([]storage.LedgerAccount, error)
}

type SettlementApplier interface {
	ApplySettlementInternal(ctx context.Context, req *ledgerpb.ApplySettlementRequest, eventID string) (*storage.SettlementResult, error)
}

type TradeConsumer struct {
	store    OrderLookup
	ledger   SettlementApplier
	producer kafka.Publisher
	logger   *slog.Logger
}

func NewTradeConsumer(store OrderLookup, ledger SettlementApplier, producer kafka.Publisher, logger *slog.Logger) *TradeConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &TradeConsumer{
		store:    store,
		ledger:   ledger,
		producer: producer,
		logger:   logger,
	}
}

func (c *TradeConsumer) HandleMessage(ctx context.Context, msg *sarama.ConsumerMessage) error {
	if msg == nil || len(msg.Value) == 0 {
		return fmt.Errorf("empty kafka message")
	}
	var event TradeExecutedEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("decode trades.executed: %w", err)
	}
	if err := event.Validate(); err != nil {
		return err
	}

	makerOrderID, err := parseUUID(event.MakerOrderID, "maker_order_id")
	if err != nil {
		return err
	}
	takerOrderID, err := parseUUID(event.TakerOrderID, "taker_order_id")
	if err != nil {
		return err
	}

	makerAccountID, err := c.store.GetOrderAccountID(ctx, makerOrderID)
	if err != nil {
		if errors.Is(err, storage.ErrOrderNotFound) {
			c.logger.Warn("order missing for trade event", "order_id", makerOrderID, "side", "maker", "trade_id", event.TradeID, "event_id", event.EventID)
			return fmt.Errorf("lookup maker account: %w", err)
		}
		return fmt.Errorf("lookup maker account: %w", err)
	}
	takerAccountID, err := c.store.GetOrderAccountID(ctx, takerOrderID)
	if err != nil {
		if errors.Is(err, storage.ErrOrderNotFound) {
			c.logger.Warn("order missing for trade event", "order_id", takerOrderID, "side", "taker", "trade_id", event.TradeID, "event_id", event.EventID)
			return fmt.Errorf("lookup taker account: %w", err)
		}
		return fmt.Errorf("lookup taker account: %w", err)
	}

	req := &ledgerpb.ApplySettlementRequest{
		TradeId:        event.TradeID,
		MakerAccountId: makerAccountID.String(),
		TakerAccountId: takerAccountID.String(),
		Symbol:         event.Symbol,
		Price:          event.Price,
		Quantity:       event.Quantity,
		MakerSide:      event.MakerSide,
	}

	result, err := c.ledger.ApplySettlementInternal(ctx, req, event.EventID)
	if err != nil {
		return err
	}
	entries := result.Entries
	balances := result.Balances
	if result.AlreadyProcessed {
		c.logger.Info("trade event already processed", "event_id", event.EventID, "trade_id", event.TradeID)
		tradeID, err := parseUUID(event.TradeID, "trade_id")
		if err != nil {
			return err
		}
		entries, err = c.store.GetEntriesByReference(ctx, tradeID)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("no ledger entries found for trade %s", event.TradeID)
		}
		balances, err = c.store.GetBalancesByAccountAssets(ctx, buildBalanceLookup(entries))
		if err != nil {
			return err
		}
		if len(balances) == 0 {
			return fmt.Errorf("no balances found for trade %s", event.TradeID)
		}
	}

	if c.producer == nil {
		return fmt.Errorf("kafka producer not configured")
	}

	correlationID := strings.TrimSpace(event.CorrelationID)
	if correlationID == "" {
		correlationID = strings.TrimSpace(event.EventID)
	}
	if correlationID == "" {
		correlationID = event.TradeID
	}

	if err := c.publishLedgerEntries(ctx, correlationID, event.TradeID, entries); err != nil {
		return err
	}
	if err := c.publishBalanceUpdates(ctx, correlationID, event.TradeID, balances); err != nil {
		return err
	}

	return nil
}

func (e *TradeExecutedEvent) Validate() error {
	if err := e.Envelope.Validate(); err != nil {
		return err
	}
	if e.EventType != tradesExecutedEventType {
		return fmt.Errorf("unexpected event_type: %s", e.EventType)
	}
	if strings.TrimSpace(e.TradeID) == "" {
		return fmt.Errorf("trade_id is required")
	}
	if strings.TrimSpace(e.Symbol) == "" {
		return fmt.Errorf("symbol is required")
	}
	if strings.TrimSpace(e.MakerOrderID) == "" {
		return fmt.Errorf("maker_order_id is required")
	}
	if strings.TrimSpace(e.TakerOrderID) == "" {
		return fmt.Errorf("taker_order_id is required")
	}
	if strings.TrimSpace(e.Price) == "" {
		return fmt.Errorf("price is required")
	}
	if strings.TrimSpace(e.Quantity) == "" {
		return fmt.Errorf("quantity is required")
	}
	if _, err := decimal.NewFromString(strings.TrimSpace(e.Price)); err != nil {
		return fmt.Errorf("price must be decimal")
	}
	if _, err := decimal.NewFromString(strings.TrimSpace(e.Quantity)); err != nil {
		return fmt.Errorf("quantity must be decimal")
	}
	side := strings.ToLower(strings.TrimSpace(e.MakerSide))
	if side != "buy" && side != "sell" {
		return fmt.Errorf("maker_side must be buy or sell")
	}
	return nil
}

func (c *TradeConsumer) publishLedgerEntries(ctx context.Context, correlationID, tradeID string, entries []storage.LedgerEntry) error {
	entriesByAccount := map[uuid.UUID][]LedgerEntryEvent{}
	for _, entry := range entries {
		entriesByAccount[entry.AccountID] = append(entriesByAccount[entry.AccountID], LedgerEntryEvent{
			EntryID:     entry.ID.String(),
			Asset:       entry.Asset,
			EntryType:   entry.EntryType,
			Amount:      entry.Amount.String(),
			ReferenceID: entry.ReferenceID.String(),
			CreatedAt:   entry.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	for accountID, accountEntries := range entriesByAccount {
		eventID := kafka.DeterministicEventID(ledgerEntriesEventType, tradeID, accountID.String())
		env, err := kafka.NewEnvelopeWithID(eventID, ledgerEntriesEventType, 1, correlationID)
		if err != nil {
			return err
		}
		payload := LedgerEntriesEvent{
			Envelope:  env,
			TradeID:   tradeID,
			AccountID: accountID.String(),
			Entries:   accountEntries,
		}
		if _, _, err := c.producer.PublishJSON(ctx, ledgerEntriesEventType, accountID.String(), payload); err != nil {
			return err
		}
	}
	return nil
}

func (c *TradeConsumer) publishBalanceUpdates(ctx context.Context, correlationID, tradeID string, balances []storage.LedgerAccount) error {
	balancesByAccount := map[uuid.UUID][]BalanceUpdate{}
	for _, balance := range balances {
		balancesByAccount[balance.AccountID] = append(balancesByAccount[balance.AccountID], BalanceUpdate{
			Asset:     balance.Asset,
			Available: balance.BalanceAvailable.String(),
			Locked:    balance.BalanceLocked.String(),
			UpdatedAt: balance.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}

	for accountID, accountBalances := range balancesByAccount {
		eventID := kafka.DeterministicEventID(balancesUpdatedEventType, tradeID, accountID.String())
		env, err := kafka.NewEnvelopeWithID(eventID, balancesUpdatedEventType, 1, correlationID)
		if err != nil {
			return err
		}
		payload := BalancesUpdatedEvent{
			Envelope:  env,
			TradeID:   tradeID,
			AccountID: accountID.String(),
			Balances:  accountBalances,
		}
		if _, _, err := c.producer.PublishJSON(ctx, balancesUpdatedEventType, accountID.String(), payload); err != nil {
			return err
		}
	}
	return nil
}

func parseUUID(value, field string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return uuid.Nil, fmt.Errorf("%s is required", field)
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s", field)
	}
	return parsed, nil
}

func buildBalanceLookup(entries []storage.LedgerEntry) []storage.LedgerAccount {
	seen := make(map[string]storage.LedgerAccount)
	for _, entry := range entries {
		key := entry.AccountID.String() + ":" + strings.ToUpper(entry.Asset)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = storage.LedgerAccount{
			AccountID: entry.AccountID,
			Asset:     entry.Asset,
		}
	}
	lookup := make([]storage.LedgerAccount, 0, len(seen))
	for _, acct := range seen {
		lookup = append(lookup, acct)
	}
	return lookup
}
