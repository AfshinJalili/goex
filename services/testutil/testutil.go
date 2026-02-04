package testutil

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func SetupTestDB() (*pgxpool.Pool, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		getEnv("POSTGRES_USER", "cex"),
		getEnv("POSTGRES_PASSWORD", "cex"),
		getEnv("POSTGRES_HOST", "localhost"),
		getEnv("POSTGRES_PORT", "5432"),
		getEnv("POSTGRES_DB", "cex_core"),
		getEnv("POSTGRES_SSLMODE", "disable"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}

func CleanupTestData(ctx context.Context, pool *pgxpool.Pool) error {
	queries := []string{
		"DELETE FROM refresh_tokens",
		"DELETE FROM api_keys",
		"DELETE FROM ledger_accounts WHERE account_id NOT IN ('00000000-0000-0000-0000-000000000101','00000000-0000-0000-0000-000000000102')",
		"DELETE FROM accounts WHERE user_id NOT IN ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002')",
		"DELETE FROM users WHERE email NOT IN ('demo@example.com', 'trader@example.com')",
	}

	for _, q := range queries {
		if _, err := pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("cleanup %q: %w", q, err)
		}
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
