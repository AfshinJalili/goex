package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	base "github.com/AfshinJalili/goex/libs/config"
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

type ServiceConfig struct {
	GRPCAddr string
}

type CacheConfig struct {
	RefreshInterval time.Duration
}

type Config struct {
	App                  base.AppConfig
	DB                   DBConfig
	GRPC                 GRPCConfig
	LedgerService        ServiceConfig
	Cache                CacheConfig
	MarketBuySlippageBps int
}

func Load() (*Config, error) {
	appCfg, err := base.Load(os.Getenv("CEX_CONFIG"))
	if err != nil {
		return nil, err
	}

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
			Port: envInt("CEX_GRPC_PORT", 9092),
		},
		LedgerService: ServiceConfig{
			GRPCAddr: envString("LEDGER_SERVICE_ADDR", "ledger:9091"),
		},
		Cache: CacheConfig{
			RefreshInterval: envDuration("CEX_CACHE_REFRESH_INTERVAL", 5*time.Minute),
		},
		MarketBuySlippageBps: envInt("MARKET_BUY_SLIPPAGE_BPS", 50),
	}

	if cfg.GRPC.Port <= 0 {
		return nil, fmt.Errorf("CEX_GRPC_PORT must be positive")
	}
	if cfg.LedgerService.GRPCAddr == "" {
		return nil, fmt.Errorf("LEDGER_SERVICE_ADDR must be set")
	}
	if cfg.MarketBuySlippageBps < 0 {
		return nil, fmt.Errorf("market_buy_slippage_bps must be non-negative")
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
