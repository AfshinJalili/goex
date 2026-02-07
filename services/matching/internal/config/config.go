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
	OrdersCancelled string
	TradesExecuted  string
}

type KafkaConfig struct {
	Brokers       []string
	ConsumerGroup string
	Topics        KafkaTopics
}

type Config struct {
	App   base.AppConfig
	DB    DBConfig
	GRPC  GRPCConfig
	Kafka KafkaConfig
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
	v.SetDefault("kafka.consumer_group", "matching-engine")
	v.SetDefault("kafka.topics.orders_accepted", "orders.accepted")
	v.SetDefault("kafka.topics.orders_cancelled", "orders.cancelled")
	v.SetDefault("kafka.topics.trades_executed", "trades.executed")
	v.SetDefault("postgres.host", "localhost")
	v.SetDefault("postgres.port", 5432)
	v.SetDefault("postgres.database", "cex_core")
	v.SetDefault("postgres.user", "cex")
	v.SetDefault("postgres.password", "cex")
	v.SetDefault("postgres.sslmode", "disable")

	cfg := &Config{
		App: *appCfg,
		DB: DBConfig{
			Host:     envString("DB_HOST", envString("POSTGRES_HOST", v.GetString("postgres.host"))),
			Port:     envInt("DB_PORT", envInt("POSTGRES_PORT", v.GetInt("postgres.port"))),
			Name:     envString("DB_NAME", envString("POSTGRES_DB", v.GetString("postgres.database"))),
			User:     envString("DB_USER", envString("POSTGRES_USER", v.GetString("postgres.user"))),
			Password: envString("DB_PASSWORD", envString("POSTGRES_PASSWORD", v.GetString("postgres.password"))),
			SSLMode:  envString("DB_SSLMODE", envString("POSTGRES_SSLMODE", v.GetString("postgres.sslmode"))),
		},
		GRPC: GRPCConfig{
			Host: envString("GRPC_HOST", "0.0.0.0"),
			Port: envInt("GRPC_PORT", v.GetInt("grpc.port")),
		},
		Kafka: KafkaConfig{
			Brokers:       envCSV("KAFKA_BROKERS", v.GetStringSlice("kafka.brokers")),
			ConsumerGroup: envString("KAFKA_CONSUMER_GROUP", v.GetString("kafka.consumer_group")),
			Topics: KafkaTopics{
				OrdersAccepted:  envString("KAFKA_ORDERS_ACCEPTED_TOPIC", v.GetString("kafka.topics.orders_accepted")),
				OrdersCancelled: envString("KAFKA_ORDERS_CANCELLED_TOPIC", v.GetString("kafka.topics.orders_cancelled")),
				TradesExecuted:  envString("KAFKA_TRADES_TOPIC", v.GetString("kafka.topics.trades_executed")),
			},
		},
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
	if cfg.Kafka.Topics.OrdersAccepted == "" || cfg.Kafka.Topics.OrdersCancelled == "" || cfg.Kafka.Topics.TradesExecuted == "" {
		return nil, fmt.Errorf("kafka topics required")
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
