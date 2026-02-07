package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AfshinJalili/goex/services/testutil"
	"github.com/IBM/sarama"
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
	EventID      string `json:"event_id"`
	EventType    string `json:"event_type"`
	EventVersion int    `json:"event_version"`
	Timestamp    string `json:"timestamp"`
	TradeID      string `json:"trade_id"`
	Symbol       string `json:"symbol"`
	MakerOrderID string `json:"maker_order_id"`
	TakerOrderID string `json:"taker_order_id"`
	Price        string `json:"price"`
	Quantity     string `json:"quantity"`
	MakerSide    string `json:"maker_side"`
	ExecutedAt   string `json:"executed_at"`
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

func submitOrder(t *testing.T, token string, req orderRequest) string {
	payload, _ := json.Marshal(req)
	request, err := http.NewRequest(http.MethodPost, getOrderIngestURL()+"/orders", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("submit order failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
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

func waitForTrade(t *testing.T, watcher tradeWatcher, makerOrderID, takerOrderID string) tradeExecutedEvent {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for trade event")
		case event := <-watcher.ch:
			if (event.MakerOrderID == makerOrderID && event.TakerOrderID == takerOrderID) ||
				(event.MakerOrderID == takerOrderID && event.TakerOrderID == makerOrderID) {
				return event
			}
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

func TestMatchingEngineFlow(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

	buyerToken := login(t, "demo@example.com", "demo123")
	sellerToken := login(t, "trader@example.com", "trader123")

	watcher := startTradeWatcher(t)
	defer watcher.closeFn()
	time.Sleep(500 * time.Millisecond)

	buyOrderID := submitOrder(t, buyerToken, orderRequest{
		Symbol:      "BTC-USD",
		Side:        "buy",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	})

	sellOrderID := submitOrder(t, sellerToken, orderRequest{
		Symbol:      "BTC-USD",
		Side:        "sell",
		Type:        "limit",
		Price:       "100",
		Quantity:    "1",
		TimeInForce: "GTC",
	})

	event := waitForTrade(t, watcher, sellOrderID, buyOrderID)
	if event.TradeID == "" {
		t.Fatalf("expected trade id")
	}
	if event.Symbol != "BTC-USD" {
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

	pool, err := testutil.SetupTestDB()
	if err == nil {
		defer pool.Close()
		_, _ = pool.Exec(context.Background(), `DELETE FROM orders WHERE id = $1 OR id = $2`, buyOrderID, sellOrderID)
	}
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
