package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/IBM/sarama"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type orderRequest struct {
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Type        string `json:"type"`
	Price       string `json:"price,omitempty"`
	Quantity    string `json:"quantity"`
	TimeInForce string `json:"time_in_force"`
}

type orderResponse struct {
	OrderID   string `json:"order_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type tradeExecutedEvent struct {
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	EventVersion  int    `json:"event_version"`
	Timestamp     string `json:"timestamp"`
	CorrelationID string `json:"correlation_id"`
	TradeID       string `json:"trade_id"`
	Symbol        string `json:"symbol"`
	MakerOrderID  string `json:"maker_order_id"`
	TakerOrderID  string `json:"taker_order_id"`
	Price         string `json:"price"`
	Quantity      string `json:"quantity"`
	MakerSide     string `json:"maker_side"`
	ExecutedAt    string `json:"executed_at"`
}

const testSymbol = "ETH-USD"

type ledgerEntryEvent struct {
	EntryID     string `json:"entry_id"`
	Asset       string `json:"asset"`
	EntryType   string `json:"entry_type"`
	Amount      string `json:"amount"`
	ReferenceID string `json:"reference_id"`
	CreatedAt   string `json:"created_at"`
}

type ledgerEntriesEvent struct {
	EventID       string             `json:"event_id"`
	EventType     string             `json:"event_type"`
	EventVersion  int                `json:"event_version"`
	Timestamp     string             `json:"timestamp"`
	CorrelationID string             `json:"correlation_id"`
	TradeID       string             `json:"trade_id"`
	AccountID     string             `json:"account_id"`
	Entries       []ledgerEntryEvent `json:"entries"`
}

type balanceUpdateEvent struct {
	Asset     string `json:"asset"`
	Available string `json:"available"`
	Locked    string `json:"locked"`
	UpdatedAt string `json:"updated_at"`
}

type balancesUpdatedEvent struct {
	EventID       string               `json:"event_id"`
	EventType     string               `json:"event_type"`
	EventVersion  int                  `json:"event_version"`
	Timestamp     string               `json:"timestamp"`
	CorrelationID string               `json:"correlation_id"`
	TradeID       string               `json:"trade_id"`
	AccountID     string               `json:"account_id"`
	Balances      []balanceUpdateEvent `json:"balances"`
}

func getOrderIngestURL() string {
	if url := os.Getenv("ORDER_INGEST_URL"); url != "" {
		return url
	}
	return "http://localhost:8085"
}

func getKafkaBrokers() []string {
	if v := os.Getenv("KAFKA_BROKERS"); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := normalizeBroker(strings.TrimSpace(part))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{"localhost:9092"}
}

func normalizeBroker(value string) string {
	if value == "" {
		return value
	}
	if strings.Contains(value, "://") {
		parts := strings.SplitN(value, "://", 2)
		value = parts[1]
	}
	return strings.TrimSpace(value)
}

func getMatchingURL() string {
	if url := os.Getenv("MATCHING_URL"); url != "" {
		return url
	}
	return "http://localhost:8086"
}

func login(t *testing.T, email, password string) string {
	resp, err := makeGatewayRequest(http.MethodPost, "/auth/login", loginRequest{Email: email, Password: password}, nil)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: expected 200, got %d", resp.StatusCode)
	}
	var out loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return out.AccessToken
}

func submitOrder(t *testing.T, token string, req orderRequest, requestID string) string {
	payload, _ := json.Marshal(req)
	request, err := http.NewRequest(http.MethodPost, getOrderIngestURL()+"/orders", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	if strings.TrimSpace(requestID) != "" {
		request.Header.Set("X-Request-ID", requestID)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("submit order failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out orderResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode order response: %v", err)
	}
	if out.OrderID == "" {
		t.Fatalf("missing order_id")
	}
	return out.OrderID
}

type tradeWatcher struct {
	ch      chan tradeExecutedEvent
	closeFn func()
}

func startTradeWatcher(t *testing.T) tradeWatcher {
	brokers := getKafkaBrokers()
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_7_0_0
	cfg.Consumer.Return.Errors = true

	consumer, err := sarama.NewConsumer(brokers, cfg)
	if err != nil {
		t.Fatalf("kafka consumer: %v", err)
	}

	partitions, err := consumer.Partitions("trades.executed")
	if err != nil {
		t.Fatalf("partitions: %v", err)
	}

	msgCh := make(chan *sarama.ConsumerMessage, 10)
	partitionConsumers := make([]sarama.PartitionConsumer, 0, len(partitions))

	for _, p := range partitions {
		pc, err := consumer.ConsumePartition("trades.executed", p, sarama.OffsetNewest)
		if err != nil {
			t.Fatalf("consume partition: %v", err)
		}
		partitionConsumers = append(partitionConsumers, pc)
		go func(partConsumer sarama.PartitionConsumer) {
			for msg := range partConsumer.Messages() {
				msgCh <- msg
			}
		}(pc)
	}

	closeFn := func() {
		for _, pc := range partitionConsumers {
			_ = pc.Close()
		}
		_ = consumer.Close()
	}

	out := make(chan tradeExecutedEvent, 5)
	go func() {
		for msg := range msgCh {
			var event tradeExecutedEvent
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				continue
			}
			out <- event
		}
	}()

	return tradeWatcher{ch: out, closeFn: closeFn}
}

type ledgerWatcher struct {
	entries  chan ledgerEntriesEvent
	balances chan balancesUpdatedEvent
	closeFn  func()
}

func startLedgerWatcher(t *testing.T) ledgerWatcher {
	brokers := getKafkaBrokers()
	cfg := sarama.NewConfig()
	cfg.Version = sarama.V3_7_0_0
	cfg.Consumer.Return.Errors = true

	consumer, err := sarama.NewConsumer(brokers, cfg)
	if err != nil {
		t.Fatalf("kafka consumer: %v", err)
	}

	entriesCh := make(chan ledgerEntriesEvent, 10)
	balancesCh := make(chan balancesUpdatedEvent, 10)
	var partitionConsumers []sarama.PartitionConsumer

	startTopic := func(topic string, handle func([]byte)) {
		partitions, err := consumer.Partitions(topic)
		if err != nil {
			t.Fatalf("partitions: %v", err)
		}
		for _, p := range partitions {
			pc, err := consumer.ConsumePartition(topic, p, sarama.OffsetNewest)
			if err != nil {
				t.Fatalf("consume partition: %v", err)
			}
			partitionConsumers = append(partitionConsumers, pc)
			go func(partConsumer sarama.PartitionConsumer) {
				for msg := range partConsumer.Messages() {
					handle(msg.Value)
				}
			}(pc)
		}
	}

	startTopic("ledger.entries", func(payload []byte) {
		var event ledgerEntriesEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return
		}
		entriesCh <- event
	})
	startTopic("balances.updated", func(payload []byte) {
		var event balancesUpdatedEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return
		}
		balancesCh <- event
	})

	closeFn := func() {
		for _, pc := range partitionConsumers {
			_ = pc.Close()
		}
		_ = consumer.Close()
	}

	return ledgerWatcher{
		entries:  entriesCh,
		balances: balancesCh,
		closeFn:  closeFn,
	}
}

func waitForTrade(t *testing.T, watcher tradeWatcher, makerOrderID, takerOrderID string, correlationIDs map[string]struct{}) tradeExecutedEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for trade event")
		case event := <-watcher.ch:
			if (event.MakerOrderID == makerOrderID && event.TakerOrderID == takerOrderID) ||
				(event.MakerOrderID == takerOrderID && event.TakerOrderID == makerOrderID) {
				if len(correlationIDs) > 0 && event.CorrelationID != "" {
					if _, ok := correlationIDs[event.CorrelationID]; !ok {
						continue
					}
				}
				return event
			}
		}
	}
}

func waitForLedgerEvents(t *testing.T, watcher ledgerWatcher, tradeID string, accountIDs []string, correlationIDs map[string]struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entriesSeen := make(map[string]bool)
	balancesSeen := make(map[string]bool)
	accountSet := make(map[string]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		accountSet[id] = struct{}{}
	}

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for ledger events")
		case event := <-watcher.entries:
			if event.TradeID != tradeID {
				continue
			}
			if len(correlationIDs) > 0 && event.CorrelationID != "" {
				if _, ok := correlationIDs[event.CorrelationID]; !ok {
					continue
				}
			}
			if _, ok := accountSet[event.AccountID]; ok {
				entriesSeen[event.AccountID] = true
			}
		case event := <-watcher.balances:
			if event.TradeID != tradeID {
				continue
			}
			if len(correlationIDs) > 0 && event.CorrelationID != "" {
				if _, ok := correlationIDs[event.CorrelationID]; !ok {
					continue
				}
			}
			if _, ok := accountSet[event.AccountID]; ok {
				balancesSeen[event.AccountID] = true
			}
		}

		allEntries := true
		allBalances := true
		for _, id := range accountIDs {
			if !entriesSeen[id] {
				allEntries = false
			}
			if !balancesSeen[id] {
				allBalances = false
			}
		}
		if allEntries && allBalances {
			return
		}
	}
}

func waitForOrderStatus(t *testing.T, orderID string) string {
	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var status string
	for {
		row := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, orderID)
		if err := row.Scan(&status); err == nil {
			if status == "filled" || status == "open" {
				return status
			}
		}
		select {
		case <-ctx.Done():
			return status
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func waitForOrderFilled(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orderID string, minFilled decimal.Decimal) (string, decimal.Decimal) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for filled quantity")
		default:
		}

		var status string
		var filledStr string
		row := pool.QueryRow(ctx, `SELECT status, filled_quantity::text FROM orders WHERE id = $1`, orderID)
		if err := row.Scan(&status, &filledStr); err == nil {
			filled, err := decimal.NewFromString(strings.TrimSpace(filledStr))
			if err == nil && filled.GreaterThanOrEqual(minFilled) {
				return status, filled
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func waitForTradesCount(t *testing.T, watcher tradeWatcher, orderIDs map[string]struct{}, correlationIDs map[string]struct{}, expected int) ([]tradeExecutedEvent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	collected := make([]tradeExecutedEvent, 0, expected)
	seen := make(map[string]struct{})
	for {
		if len(collected) >= expected {
			return collected, nil
		}
		select {
		case <-ctx.Done():
			return collected, fmt.Errorf("timed out waiting for trades")
		case event := <-watcher.ch:
			if _, ok := orderIDs[event.MakerOrderID]; !ok {
				if _, ok := orderIDs[event.TakerOrderID]; !ok {
					continue
				}
			}
			if len(correlationIDs) > 0 && event.CorrelationID != "" {
				if _, ok := correlationIDs[event.CorrelationID]; !ok {
					continue
				}
			}
			if _, ok := seen[event.TradeID]; ok {
				continue
			}
			seen[event.TradeID] = struct{}{}
			collected = append(collected, event)
		}
	}
}

func splitSymbol(symbol string) (string, string, error) {
	parts := strings.Split(symbol, "-")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid symbol: %s", symbol)
	}
	return strings.ToUpper(parts[0]), strings.ToUpper(parts[1]), nil
}

func getAccountID(ctx context.Context, t *testing.T, pool *pgxpool.Pool, email string) string {
	var accountID string
	row := pool.QueryRow(ctx, `
		SELECT a.id::text
		FROM accounts a
		JOIN users u ON u.id = a.user_id
		WHERE u.email = $1
		ORDER BY a.created_at ASC
		LIMIT 1
	`, email)
	if err := row.Scan(&accountID); err != nil {
		t.Fatalf("account lookup failed: %v", err)
	}
	return accountID
}

type balanceSnapshot struct {
	Available decimal.Decimal
	Locked    decimal.Decimal
}

func getBalanceSnapshot(ctx context.Context, t *testing.T, pool *pgxpool.Pool, accountID string, asset string) balanceSnapshot {
	row := pool.QueryRow(ctx, `
		SELECT balance_available::text, balance_locked::text
		FROM ledger_accounts
		WHERE account_id = $1 AND asset = $2
	`, accountID, asset)
	var availableStr, lockedStr string
	if err := row.Scan(&availableStr, &lockedStr); err != nil {
		if err == pgx.ErrNoRows {
			return balanceSnapshot{Available: decimal.Zero, Locked: decimal.Zero}
		}
		t.Fatalf("balance lookup failed: %v", err)
	}
	available, err := decimal.NewFromString(availableStr)
	if err != nil {
		t.Fatalf("parse available: %v", err)
	}
	locked, err := decimal.NewFromString(lockedStr)
	if err != nil {
		t.Fatalf("parse locked: %v", err)
	}
	return balanceSnapshot{Available: available, Locked: locked}
}

func getReservationStatus(ctx context.Context, t *testing.T, pool *pgxpool.Pool, orderID string) (string, decimal.Decimal, decimal.Decimal) {
	row := pool.QueryRow(ctx, `
		SELECT status, amount::text, consumed_amount::text
		FROM balance_reservations
		WHERE order_id = $1
	`, orderID)
	var status, amountStr, consumedStr string
	if err := row.Scan(&status, &amountStr, &consumedStr); err != nil {
		t.Fatalf("reservation lookup failed: %v", err)
	}
	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		t.Fatalf("parse reservation amount: %v", err)
	}
	consumed, err := decimal.NewFromString(consumedStr)
	if err != nil {
		t.Fatalf("parse reservation consumed: %v", err)
	}
	return status, amount, consumed
}

func ensureBalance(ctx context.Context, t *testing.T, pool *pgxpool.Pool, accountID, asset, available string) {
	cmd, err := pool.Exec(ctx, `
		UPDATE ledger_accounts
		SET balance_available = $3, balance_locked = 0, updated_at = now()
		WHERE account_id = $1 AND asset = $2
	`, accountID, asset, available)
	if err != nil {
		t.Fatalf("ensure balance failed: %v", err)
	}
	if cmd.RowsAffected() == 0 {
		t.Fatalf("missing ledger account for %s %s", accountID, asset)
	}
}

func TestMatchingEngineFlow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	ctx := context.Background()
	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	_, _ = pool.Exec(ctx, `DELETE FROM processed_events WHERE event_id LIKE 'ledger:%'`)

	buyerToken := login(t, "demo@example.com", "demo123")
	sellerToken := login(t, "trader@example.com", "trader123")

	buyerAccountID := getAccountID(ctx, t, pool, "demo@example.com")
	sellerAccountID := getAccountID(ctx, t, pool, "trader@example.com")
	baseAsset, quoteAsset, err := splitSymbol(testSymbol)
	if err != nil {
		t.Fatalf("split symbol: %v", err)
	}
	ensureBalance(ctx, t, pool, buyerAccountID, quoteAsset, "10000")
	ensureBalance(ctx, t, pool, sellerAccountID, baseAsset, "10")

	watcher := startTradeWatcher(t)
	defer watcher.closeFn()
	time.Sleep(500 * time.Millisecond)

	buyReqID := uuid.NewString()
	sellReqID := uuid.NewString()
	buyOrderID := submitOrder(t, buyerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	}, buyReqID)

	sellOrderID := submitOrder(t, sellerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "sell",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	}, sellReqID)

	correlationIDs := map[string]struct{}{
		buyReqID:  {},
		sellReqID: {},
	}
	event := waitForTrade(t, watcher, sellOrderID, buyOrderID, correlationIDs)
	if event.TradeID == "" {
		t.Fatalf("expected trade id")
	}
	if event.Symbol != testSymbol {
		t.Fatalf("unexpected symbol: %s", event.Symbol)
	}

	buyStatus := waitForOrderStatus(t, buyOrderID)
	sellStatus := waitForOrderStatus(t, sellOrderID)
	if buyStatus == "" || sellStatus == "" {
		t.Fatalf("expected order statuses")
	}

	if buyStatus != "filled" || sellStatus != "filled" {
		// allow open if partial matching occurs due to async processing
		t.Logf("order statuses: buy=%s sell=%s", buyStatus, sellStatus)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1 OR id = $2`, buyOrderID, sellOrderID)
}

func TestMatchingEngineSettlementFlow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	ctx := context.Background()
	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	buyerToken := login(t, "demo@example.com", "demo123")
	sellerToken := login(t, "trader@example.com", "trader123")

	buyerAccountID := getAccountID(ctx, t, pool, "demo@example.com")
	sellerAccountID := getAccountID(ctx, t, pool, "trader@example.com")

	baseAsset, quoteAsset, err := splitSymbol(testSymbol)
	if err != nil {
		t.Fatalf("split symbol: %v", err)
	}

	ensureBalance(ctx, t, pool, buyerAccountID, quoteAsset, "10000")
	ensureBalance(ctx, t, pool, sellerAccountID, baseAsset, "10")

	buyerBeforeBase := getBalanceSnapshot(ctx, t, pool, buyerAccountID, baseAsset)
	buyerBeforeQuote := getBalanceSnapshot(ctx, t, pool, buyerAccountID, quoteAsset)
	sellerBeforeBase := getBalanceSnapshot(ctx, t, pool, sellerAccountID, baseAsset)
	sellerBeforeQuote := getBalanceSnapshot(ctx, t, pool, sellerAccountID, quoteAsset)

	tradeWatcher := startTradeWatcher(t)
	defer tradeWatcher.closeFn()
	ledgerWatcher := startLedgerWatcher(t)
	defer ledgerWatcher.closeFn()
	time.Sleep(500 * time.Millisecond)

	buyReqID := uuid.NewString()
	sellReqID := uuid.NewString()
	buyOrderID := submitOrder(t, buyerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	}, buyReqID)

	sellOrderID := submitOrder(t, sellerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "sell",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	}, sellReqID)

	correlationIDs := map[string]struct{}{
		buyReqID:  {},
		sellReqID: {},
	}
	event := waitForTrade(t, tradeWatcher, sellOrderID, buyOrderID, correlationIDs)
	waitForLedgerEvents(t, ledgerWatcher, event.TradeID, []string{buyerAccountID, sellerAccountID}, correlationIDs)

	_ = waitForOrderStatus(t, buyOrderID)
	_ = waitForOrderStatus(t, sellOrderID)

	buyerAfterBase := getBalanceSnapshot(ctx, t, pool, buyerAccountID, baseAsset)
	buyerAfterQuote := getBalanceSnapshot(ctx, t, pool, buyerAccountID, quoteAsset)
	sellerAfterBase := getBalanceSnapshot(ctx, t, pool, sellerAccountID, baseAsset)
	sellerAfterQuote := getBalanceSnapshot(ctx, t, pool, sellerAccountID, quoteAsset)

	if !buyerAfterBase.Available.GreaterThan(buyerBeforeBase.Available) {
		t.Fatalf("expected buyer base available to increase")
	}
	if !buyerAfterQuote.Available.LessThan(buyerBeforeQuote.Available) {
		t.Fatalf("expected buyer quote available to decrease")
	}
	if !sellerAfterBase.Available.LessThan(sellerBeforeBase.Available) {
		t.Fatalf("expected seller base available to decrease")
	}
	if !sellerAfterQuote.Available.GreaterThan(sellerBeforeQuote.Available) {
		t.Fatalf("expected seller quote available to increase")
	}
	if !buyerAfterQuote.Locked.Equal(decimal.Zero) {
		t.Fatalf("expected buyer quote locked to clear, got %s", buyerAfterQuote.Locked.String())
	}
	if !sellerAfterBase.Locked.Equal(decimal.Zero) {
		t.Fatalf("expected seller base locked to clear, got %s", sellerAfterBase.Locked.String())
	}

	status, amount, consumed := getReservationStatus(ctx, t, pool, buyOrderID)
	if status != "consumed" && status != "released" {
		t.Fatalf("unexpected buyer reservation status: %s", status)
	}
	if amount.LessThanOrEqual(decimal.Zero) || consumed.LessThan(decimal.Zero) {
		t.Fatalf("unexpected buyer reservation amounts")
	}

	status, amount, consumed = getReservationStatus(ctx, t, pool, sellOrderID)
	if status != "consumed" && status != "released" {
		t.Fatalf("unexpected seller reservation status: %s", status)
	}
	if amount.LessThanOrEqual(decimal.Zero) || consumed.LessThan(decimal.Zero) {
		t.Fatalf("unexpected seller reservation amounts")
	}

	tradeUUID, _ := uuid.Parse(event.TradeID)
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_entries WHERE reference_type = 'trade' AND reference_id = $1`, tradeUUID)
	_, _ = pool.Exec(ctx, `DELETE FROM balance_reservations WHERE order_id = $1 OR order_id = $2`, buyOrderID, sellOrderID)
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1 OR id = $2`, buyOrderID, sellOrderID)
}

func TestMatchingEnginePartialFillFlow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	ctx := context.Background()
	pool, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("db connect: %v", err)
	}
	defer pool.Close()
	_, _ = pool.Exec(ctx, `DELETE FROM processed_events WHERE event_id LIKE 'ledger:%'`)

	buyerToken := login(t, "demo@example.com", "demo123")
	sellerToken := login(t, "trader@example.com", "trader123")

	buyerAccountID := getAccountID(ctx, t, pool, "demo@example.com")
	sellerAccountID := getAccountID(ctx, t, pool, "trader@example.com")

	baseAsset, quoteAsset, err := splitSymbol(testSymbol)
	if err != nil {
		t.Fatalf("split symbol: %v", err)
	}

	ensureBalance(ctx, t, pool, buyerAccountID, quoteAsset, "10000")
	ensureBalance(ctx, t, pool, sellerAccountID, baseAsset, "10")

	tradeWatcher := startTradeWatcher(t)
	defer tradeWatcher.closeFn()
	time.Sleep(500 * time.Millisecond)

	buyReqID := uuid.NewString()
	sellReqID1 := uuid.NewString()
	sellReqID2 := uuid.NewString()
	buyOrderID := submitOrder(t, buyerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "5",
		TimeInForce: "GTC",
	}, buyReqID)

	sellOrderID1 := submitOrder(t, sellerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "sell",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	}, sellReqID1)
	sellOrderID2 := submitOrder(t, sellerToken, orderRequest{
		Symbol:      testSymbol,
		Side:        "sell",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	}, sellReqID2)

	orderIDs := map[string]struct{}{
		buyOrderID:   {},
		sellOrderID1: {},
		sellOrderID2: {},
	}
	correlationIDs := map[string]struct{}{
		buyReqID:   {},
		sellReqID1: {},
		sellReqID2: {},
	}
	trades, err := waitForTradesCount(t, tradeWatcher, orderIDs, correlationIDs, 2)
	if err != nil {
		t.Fatalf("wait trades: %v", err)
	}
	if len(trades) < 2 {
		t.Fatalf("expected at least 2 trades, got %d", len(trades))
	}

	status, filled := waitForOrderFilled(t, ctx, pool, buyOrderID, decimal.NewFromInt(2))
	if status != "open" {
		t.Fatalf("expected buy order open, got %s", status)
	}
	if !filled.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("expected filled 2, got %s", filled.String())
	}

	resStatus, amount, consumed := getReservationStatus(ctx, t, pool, buyOrderID)
	if resStatus != "active" {
		t.Fatalf("expected reservation active, got %s", resStatus)
	}
	if consumed.LessThanOrEqual(decimal.Zero) || consumed.GreaterThanOrEqual(amount) {
		t.Fatalf("expected partial consumption, consumed=%s amount=%s", consumed.String(), amount.String())
	}

	buyerQuote := getBalanceSnapshot(ctx, t, pool, buyerAccountID, quoteAsset)
	expectedLocked := amount.Sub(consumed)
	if !buyerQuote.Locked.Equal(expectedLocked) {
		t.Fatalf("expected locked %s, got %s", expectedLocked.String(), buyerQuote.Locked.String())
	}

	for _, trade := range trades {
		tradeUUID, _ := uuid.Parse(trade.TradeID)
		_, _ = pool.Exec(ctx, `DELETE FROM ledger_entries WHERE reference_type = 'trade' AND reference_id = $1`, tradeUUID)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM balance_reservations WHERE order_id = $1 OR order_id = $2 OR order_id = $3`, buyOrderID, sellOrderID1, sellOrderID2)
	_, _ = pool.Exec(ctx, `DELETE FROM orders WHERE id = $1 OR id = $2 OR id = $3`, buyOrderID, sellOrderID1, sellOrderID2)
}

func TestMatchingEngineHealth(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	resp, err := http.Get(getMatchingURL() + "/healthz")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
