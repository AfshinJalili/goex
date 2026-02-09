package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	base "github.com/AfshinJalili/goex/libs/config"
	"github.com/spf13/viper"
)

type DBConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
}

type GRPCConfig struct {
	Host string
	Port int
}

type KafkaTopics struct {
	OrdersAccepted  string
	OrdersRejected  string
	OrdersCancelled string
	TradesExecuted  string
	DeadLetter      string
}

type KafkaConfig struct {
	Brokers       []string
	ConsumerGroup string
	Topics        KafkaTopics
}

type ServiceConfig struct {
	GRPCAddr string
}

type Config struct {
	App                  base.AppConfig
	DB                   DBConfig
	GRPC                 GRPCConfig
	Kafka                KafkaConfig
	RiskService          ServiceConfig
	LedgerService        ServiceConfig
	JWTSecret            string
	MarketBuySlippageBps int
}

func Load() (*Config, error) {
	appCfg, err := base.Load(os.Getenv("CEX_CONFIG"))
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetEnvPrefix("CEX")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	path := os.Getenv("CEX_CONFIG")
	if path == "" {
		path = "config.yaml"
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.consumer_group", "order-ingest-service")
	v.SetDefault("kafka.topics.orders_accepted", "orders.accepted")
	v.SetDefault("kafka.topics.orders_rejected", "orders.rejected")
	v.SetDefault("kafka.topics.orders_cancelled", "orders.cancelled")
	v.SetDefault("kafka.topics.trades_executed", "trades.executed")
	v.SetDefault("kafka.topics.dead_letter", "dead_letter")
	v.SetDefault("risk_service.grpc_addr", "risk:9090")
	v.SetDefault("ledger_service.grpc_addr", "ledger:9091")
	v.SetDefault("jwt_secret", "")
	v.SetDefault("market_buy_slippage_bps", 50)

	kafkaBrokers := envCSV("KAFKA_BROKERS", v.GetStringSlice("kafka.brokers"))
	kafkaConsumer := envString("KAFKA_CONSUMER_GROUP", v.GetString("kafka.consumer_group"))
	topicAccepted := envString("KAFKA_ORDERS_ACCEPTED_TOPIC", v.GetString("kafka.topics.orders_accepted"))
	topicRejected := envString("KAFKA_ORDERS_REJECTED_TOPIC", v.GetString("kafka.topics.orders_rejected"))
	topicCancelled := envString("KAFKA_ORDERS_CANCELLED_TOPIC", v.GetString("kafka.topics.orders_cancelled"))
	topicTrades := envString("KAFKA_TRADES_TOPIC", v.GetString("kafka.topics.trades_executed"))
	topicDLQ := envString("KAFKA_DLQ_TOPIC", v.GetString("kafka.topics.dead_letter"))
	riskAddr := envString("RISK_SERVICE_GRPC_ADDR", v.GetString("risk_service.grpc_addr"))
	ledgerAddr := envString("LEDGER_SERVICE_GRPC_ADDR", v.GetString("ledger_service.grpc_addr"))
	jwtSecret := envString("JWT_SECRET", v.GetString("jwt_secret"))
	marketSlippage := envInt("MARKET_BUY_SLIPPAGE_BPS", v.GetInt("market_buy_slippage_bps"))

	cfg := &Config{
		App: *appCfg,
		DB: DBConfig{
			Host:     envString("DB_HOST", envString("POSTGRES_HOST", "localhost")),
			Port:     envInt("DB_PORT", envInt("POSTGRES_PORT", 5432)),
			Name:     envString("DB_NAME", envString("POSTGRES_DB", "cex_core")),
			User:     envString("DB_USER", envString("POSTGRES_USER", "cex")),
			Password: envString("DB_PASSWORD", envString("POSTGRES_PASSWORD", "cex")),
			SSLMode:  envString("DB_SSLMODE", envString("POSTGRES_SSLMODE", "disable")),
		},
		GRPC: GRPCConfig{
			Host: envString("GRPC_HOST", "0.0.0.0"),
			Port: envInt("GRPC_PORT", 9093),
		},
		Kafka: KafkaConfig{
			Brokers:       kafkaBrokers,
			ConsumerGroup: kafkaConsumer,
			Topics: KafkaTopics{
				OrdersAccepted:  topicAccepted,
				OrdersRejected:  topicRejected,
				OrdersCancelled: topicCancelled,
				TradesExecuted:  topicTrades,
				DeadLetter:      topicDLQ,
			},
		},
		RiskService:          ServiceConfig{GRPCAddr: riskAddr},
		LedgerService:        ServiceConfig{GRPCAddr: ledgerAddr},
		JWTSecret:            jwtSecret,
		MarketBuySlippageBps: marketSlippage,
	}

	if cfg.GRPC.Port <= 0 {
		return nil, fmt.Errorf("CEX_GRPC_PORT must be positive")
	}
	if len(cfg.Kafka.Brokers) == 0 {
		return nil, fmt.Errorf("kafka brokers required")
	}
	if cfg.Kafka.ConsumerGroup == "" {
		return nil, fmt.Errorf("kafka consumer group required")
	}
	if cfg.Kafka.Topics.OrdersAccepted == "" || cfg.Kafka.Topics.OrdersRejected == "" || cfg.Kafka.Topics.OrdersCancelled == "" || cfg.Kafka.Topics.TradesExecuted == "" {
		return nil, fmt.Errorf("kafka topics required")
	}
	if cfg.RiskService.GRPCAddr == "" {
		return nil, fmt.Errorf("risk service grpc addr required")
	}
	if cfg.LedgerService.GRPCAddr == "" {
		return nil, fmt.Errorf("ledger service grpc addr required")
	}
	if cfg.MarketBuySlippageBps < 0 {
		return nil, fmt.Errorf("market_buy_slippage_bps must be non-negative")
	}

	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv("CEX_" + key); v != "" {
		return v
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv("CEX_" + key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv("CEX_" + key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envCSV(key string, def []string) []string {
	if v := os.Getenv("CEX_" + key); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if v := os.Getenv(key); v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return def
}
