package config

import (
	"fmt"
	"os"
	"strconv"

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

type Config struct {
	App       base.AppConfig
	JWTSecret string
	DB        DBConfig
}

func Load() (*Config, error) {
	appCfg, err := base.Load(os.Getenv("CEX_CONFIG"))
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		App:       *appCfg,
		JWTSecret: envString("CEX_JWT_SECRET", "change-me"),
		DB: DBConfig{
			Host:     envString("POSTGRES_HOST", "localhost"),
			Port:     envInt("POSTGRES_PORT", 5432),
			Name:     envString("POSTGRES_DB", "cex_core"),
			User:     envString("POSTGRES_USER", "cex"),
			Password: envString("POSTGRES_PASSWORD", "cex"),
			SSLMode:  envString("POSTGRES_SSLMODE", "disable"),
		},
	}

	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("CEX_JWT_SECRET must be set")
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
