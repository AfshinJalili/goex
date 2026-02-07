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
	TradesExecuted string
}

type KafkaConfig struct {
	Brokers       []string
	ConsumerGroup string
	Topics        KafkaTopics
}

type FeeServiceConfig struct {
	GRPCAddr string
}

type Config struct {
	App        base.AppConfig
	DB         DBConfig
	GRPC       GRPCConfig
	Kafka      KafkaConfig
	FeeService FeeServiceConfig
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
	v.SetDefault("kafka.consumer_group", "ledger-service")
	v.SetDefault("kafka.topics.trades_executed", "trades.executed")
	v.SetDefault("fee_service.grpc_addr", "localhost:9090")

	kafkaBrokers := envCSV("KAFKA_BROKERS", v.GetStringSlice("kafka.brokers"))
	kafkaConsumer := envString("KAFKA_CONSUMER_GROUP", v.GetString("kafka.consumer_group"))
	kafkaTradesTopic := envString("KAFKA_TRADES_TOPIC", v.GetString("kafka.topics.trades_executed"))
	feeAddr := envString("FEE_SERVICE_ADDR", v.GetString("fee_service.grpc_addr"))

	cfg := &Config{
		App: *appCfg,
		DB: DBConfig{
			Host:     envString("POSTGRES_HOST", "localhost"),
			Port:     envInt("POSTGRES_PORT", 5432),
			Name:     envString("POSTGRES_DB", "cex_core"),
			User:     envString("POSTGRES_USER", "cex"),
			Password: envString("POSTGRES_PASSWORD", "cex"),
			SSLMode:  envString("POSTGRES_SSLMODE", "disable"),
		},
		GRPC: GRPCConfig{
			Host: envString("CEX_GRPC_HOST", "0.0.0.0"),
			Port: envInt("CEX_GRPC_PORT", 9091),
		},
		Kafka: KafkaConfig{
			Brokers:       kafkaBrokers,
			ConsumerGroup: kafkaConsumer,
			Topics: KafkaTopics{
				TradesExecuted: kafkaTradesTopic,
			},
		},
		FeeService: FeeServiceConfig{
			GRPCAddr: feeAddr,
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
	if cfg.Kafka.Topics.TradesExecuted == "" {
		return nil, fmt.Errorf("kafka trades topic required")
	}
	if cfg.FeeService.GRPCAddr == "" {
		return nil, fmt.Errorf("fee service grpc addr required")
	}

	return cfg, nil
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envCSV(key string, def []string) []string {
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
