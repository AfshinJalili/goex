package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AfshinJalili/goex/libs/apikey"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

const (
	demoKeyPrefix   = "demo0001"
	demoKeySecret   = "demosecret0001"
	traderKeyPrefix = "trader0001"
	traderKeySecret = "tradersecret0001"
)

func main() {
	env := getEnv("CEX_ENV", "dev")
	if env != "dev" && env != "test" {
		log.Fatalf("refusing to seed: CEX_ENV must be 'dev' or 'test' (got '%s')", env)
	}

	host := getEnv("POSTGRES_HOST", "localhost")
	port := getEnv("POSTGRES_PORT", "5432")
	db := getEnv("POSTGRES_DB", "cex_core")
	user := getEnv("POSTGRES_USER", "cex")
	password := getEnv("POSTGRES_PASSWORD", "cex")
	sslmode := getEnv("POSTGRES_SSLMODE", "disable")

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, db, sslmode)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	fmt.Println("Seeding database...")

	if err := seedUsers(ctx, pool); err != nil {
		log.Fatalf("seed users: %v", err)
	}
	fmt.Println("✓ Users seeded")

	if err := seedAccounts(ctx, pool); err != nil {
		log.Fatalf("seed accounts: %v", err)
	}
	fmt.Println("✓ Accounts seeded")

	apiKeys, err := seedAPIKeys(ctx, pool, env)
	if err != nil {
		log.Fatalf("seed api keys: %v", err)
	}
	fmt.Println("✓ API keys seeded")

	if err := seedAssets(ctx, pool); err != nil {
		log.Fatalf("seed assets: %v", err)
	}
	fmt.Println("✓ Assets seeded")

	if err := seedMarkets(ctx, pool); err != nil {
		log.Fatalf("seed markets: %v", err)
	}
	fmt.Println("✓ Markets seeded")

	if err := seedFeeTiers(ctx, pool); err != nil {
		log.Fatalf("seed fee tiers: %v", err)
	}
	fmt.Println("✓ Fee tiers seeded")

	if err := seedLedgerAccounts(ctx, pool); err != nil {
		log.Fatalf("seed ledger accounts: %v", err)
	}
	fmt.Println("✓ Ledger accounts seeded")

	if os.Getenv("SEED_TESTDATA") == "1" {
		if err := seedTestData(ctx, pool); err != nil {
			log.Fatalf("seed test data: %v", err)
		}
		fmt.Println("✓ Test data seeded")
	}

	fmt.Println("\n=== Seed Complete ===")
	fmt.Println("\nDemo Credentials:")
	fmt.Println("  Email: demo@example.com")
	fmt.Println("  Password: demo123")
	fmt.Println("  Email: trader@example.com")
	fmt.Println("  Password: trader123")

	if env == "dev" {
		fmt.Println("\nAPI Keys (DEV ONLY):")
		for email, key := range apiKeys {
			fmt.Printf("  %s: %s\n", email, key)
		}
	}
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

type argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

func hashPassword(password string, params argon2Params) (string, error) {
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", params.Memory, params.Iterations, params.Parallelism, b64Salt, b64Hash)
	return encoded, nil
}

func seedUsers(ctx context.Context, pool *pgxpool.Pool) error {
	demoID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	traderID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	params := argon2Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}

	demoHash, err := hashPassword("demo123", params)
	if err != nil {
		return fmt.Errorf("hash demo password: %w", err)
	}

	traderHash, err := hashPassword("trader123", params)
	if err != nil {
		return fmt.Errorf("hash trader password: %w", err)
	}

	now := time.Now()

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, kyc_level, mfa_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (email) DO UPDATE
		SET status = EXCLUDED.status,
		    kyc_level = EXCLUDED.kyc_level,
		    mfa_enabled = EXCLUDED.mfa_enabled,
		    updated_at = EXCLUDED.updated_at
	`, demoID, "demo@example.com", demoHash, "active", "verified", false, now, now)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, kyc_level, mfa_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (email) DO UPDATE
		SET status = EXCLUDED.status,
		    kyc_level = EXCLUDED.kyc_level,
		    mfa_enabled = EXCLUDED.mfa_enabled,
		    updated_at = EXCLUDED.updated_at
	`, traderID, "trader@example.com", traderHash, "active", "verified", false, now, now)
	if err != nil {
		return err
	}

	return nil
}

func seedAccounts(ctx context.Context, pool *pgxpool.Pool) error {
	demoUserID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	traderUserID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	demoAccountID := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	traderAccountID := uuid.MustParse("00000000-0000-0000-0000-000000000102")

	now := time.Now()

	_, err := pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET type = EXCLUDED.type,
		    status = EXCLUDED.status
	`, demoAccountID, demoUserID, "spot", "active", now)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO accounts (id, user_id, type, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET type = EXCLUDED.type,
		    status = EXCLUDED.status
	`, traderAccountID, traderUserID, "spot", "active", now)
	if err != nil {
		return err
	}

	return nil
}

func seedAPIKeys(ctx context.Context, pool *pgxpool.Pool, env string) (map[string]string, error) {
	demoUserID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	traderUserID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	demoKeyID := uuid.MustParse("00000000-0000-0000-0000-000000000201")
	traderKeyID := uuid.MustParse("00000000-0000-0000-0000-000000000202")

	demoFullKey := fmt.Sprintf("ck_%s_%s.%s", env, demoKeyPrefix, demoKeySecret)
	demoHash := apikey.Hash(demoKeyPrefix, demoKeySecret)

	traderFullKey := fmt.Sprintf("ck_%s_%s.%s", env, traderKeyPrefix, traderKeySecret)
	traderHash := apikey.Hash(traderKeyPrefix, traderKeySecret)

	scopes := []string{"trade", "read"}
	ipWhitelist := []string{}
	ipJSON, _ := json.Marshal(ipWhitelist)
	now := time.Now()

	_, err := pool.Exec(ctx, `
		INSERT INTO api_keys (id, user_id, prefix, key_hash, scopes, ip_whitelist, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)
		ON CONFLICT (prefix) DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    key_hash = EXCLUDED.key_hash,
		    scopes = EXCLUDED.scopes,
		    ip_whitelist = EXCLUDED.ip_whitelist,
		    revoked_at = NULL,
		    updated_at = EXCLUDED.updated_at
	`, demoKeyID, demoUserID, demoKeyPrefix, demoHash, scopes, ipJSON, now, now)
	if err != nil {
		return nil, err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO api_keys (id, user_id, prefix, key_hash, scopes, ip_whitelist, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)
		ON CONFLICT (prefix) DO UPDATE
		SET user_id = EXCLUDED.user_id,
		    key_hash = EXCLUDED.key_hash,
		    scopes = EXCLUDED.scopes,
		    ip_whitelist = EXCLUDED.ip_whitelist,
		    revoked_at = NULL,
		    updated_at = EXCLUDED.updated_at
	`, traderKeyID, traderUserID, traderKeyPrefix, traderHash, scopes, ipJSON, now, now)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"demo@example.com":   demoFullKey,
		"trader@example.com": traderFullKey,
	}, nil
}

func seedAssets(ctx context.Context, pool *pgxpool.Pool) error {
	assets := []struct {
		symbol    string
		precision int
	}{
		{"BTC", 8},
		{"ETH", 8},
		{"USD", 2},
		{"USDT", 6},
	}

	for _, asset := range assets {
		_, err := pool.Exec(ctx, `
			INSERT INTO assets (symbol, precision, status)
			VALUES ($1, $2, $3)
			ON CONFLICT (symbol) DO NOTHING
		`, asset.symbol, asset.precision, "active")
		if err != nil {
			return err
		}
	}

	return nil
}

func seedMarkets(ctx context.Context, pool *pgxpool.Pool) error {
	markets := []struct {
		symbol     string
		baseAsset  string
		quoteAsset string
	}{
		{"BTC-USD", "BTC", "USD"},
		{"ETH-USD", "ETH", "USD"},
		{"BTC-USDT", "BTC", "USDT"},
		{"ETH-USDT", "ETH", "USDT"},
	}

	for _, market := range markets {
		_, err := pool.Exec(ctx, `
			INSERT INTO markets (symbol, base_asset, quote_asset, status)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (symbol) DO NOTHING
		`, market.symbol, market.baseAsset, market.quoteAsset, "active")
		if err != nil {
			return err
		}
	}

	return nil
}

func seedFeeTiers(ctx context.Context, pool *pgxpool.Pool) error {
	tiers := []struct {
		name        string
		makerFeeBps int
		takerFeeBps int
		minVolume   string
	}{
		{"vip1", 8, 15, "100000"},
		{"vip2", 5, 10, "500000"},
	}

	for _, tier := range tiers {
		_, err := pool.Exec(ctx, `
			INSERT INTO fee_tiers (name, maker_fee_bps, taker_fee_bps, min_volume)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (name) DO UPDATE
			SET maker_fee_bps = EXCLUDED.maker_fee_bps,
			    taker_fee_bps = EXCLUDED.taker_fee_bps,
			    min_volume = EXCLUDED.min_volume
		`, tier.name, tier.makerFeeBps, tier.takerFeeBps, tier.minVolume)
		if err != nil {
			return err
		}
	}

	return nil
}

func seedLedgerAccounts(ctx context.Context, pool *pgxpool.Pool) error {
	demoAccountID := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	traderAccountID := uuid.MustParse("00000000-0000-0000-0000-000000000102")

	assets := []string{"BTC", "ETH", "USD", "USDT"}
	demoBalances := map[string]string{
		"BTC":  "10",
		"ETH":  "100",
		"USD":  "100000",
		"USDT": "50000",
	}
	traderBalances := map[string]string{
		"BTC":  "5",
		"ETH":  "50",
		"USD":  "50000",
		"USDT": "25000",
	}

	now := time.Now()

	for _, asset := range assets {
		_, err := pool.Exec(ctx, `
			INSERT INTO ledger_accounts (account_id, asset, balance_available, balance_locked, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (account_id, asset) DO UPDATE
			SET balance_available = EXCLUDED.balance_available,
			    balance_locked = EXCLUDED.balance_locked,
			    updated_at = EXCLUDED.updated_at
		`, demoAccountID, asset, demoBalances[asset], "0", now)
		if err != nil {
			return err
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO ledger_accounts (account_id, asset, balance_available, balance_locked, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (account_id, asset) DO UPDATE
			SET balance_available = EXCLUDED.balance_available,
			    balance_locked = EXCLUDED.balance_locked,
			    updated_at = EXCLUDED.updated_at
		`, traderAccountID, asset, traderBalances[asset], "0", now)
		if err != nil {
			return err
		}
	}

	return nil
}
