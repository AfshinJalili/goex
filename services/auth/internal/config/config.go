package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	base "github.com/AfshinJalili/goex/libs/config"
)

type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

type DBConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
}

type RateLimitRedisConfig struct {
	Addr     string
	Password string
	DB       int
	Prefix   string
}

type RateLimitConfig struct {
	LoginLimit int
	Window     time.Duration
	Redis      RateLimitRedisConfig
}

type Config struct {
	App             base.AppConfig
	JWTSecret       string
	JWTIssuer       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	Argon2          Argon2Params
	DB              DBConfig
	RateLimit       RateLimitConfig
}

func Load() (*Config, error) {
	appCfg, err := base.Load(os.Getenv("CEX_CONFIG"))
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		App:             *appCfg,
		JWTSecret:       envString("CEX_JWT_SECRET", ""),
		JWTIssuer:       envString("CEX_JWT_ISSUER", "cex-auth"),
		AccessTokenTTL:  envDuration("CEX_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: envDuration("CEX_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		Argon2: Argon2Params{
			Memory:      uint32(envInt("CEX_ARGON2_MEMORY", 64*1024)),
			Iterations:  uint32(envInt("CEX_ARGON2_ITERATIONS", 3)),
			Parallelism: uint8(envInt("CEX_ARGON2_PARALLELISM", 2)),
			SaltLength:  uint32(envInt("CEX_ARGON2_SALT_LENGTH", 16)),
			KeyLength:   uint32(envInt("CEX_ARGON2_KEY_LENGTH", 32)),
		},
		DB: DBConfig{
			Host:     envString("POSTGRES_HOST", "localhost"),
			Port:     envInt("POSTGRES_PORT", 5432),
			Name:     envString("POSTGRES_DB", "cex_core"),
			User:     envString("POSTGRES_USER", "cex"),
			Password: envString("POSTGRES_PASSWORD", "cex"),
			SSLMode:  envString("POSTGRES_SSLMODE", "disable"),
		},
		RateLimit: RateLimitConfig{
			LoginLimit: envInt("CEX_LOGIN_RATE_LIMIT", 10),
			Window:     envDuration("CEX_LOGIN_RATE_WINDOW", 1*time.Minute),
			Redis: RateLimitRedisConfig{
				Addr:     envString("CEX_RATE_LIMIT_REDIS_ADDR", ""),
				Password: envString("CEX_RATE_LIMIT_REDIS_PASSWORD", ""),
				DB:       envInt("CEX_RATE_LIMIT_REDIS_DB", 0),
				Prefix:   envString("CEX_RATE_LIMIT_REDIS_PREFIX", "cex:auth:rl:"),
			},
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

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
