package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAPIKeyLifecycleIntegration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run")
	}

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
		t.Skipf("db connection failed: %v", err)
	}
	defer pool.Close()

	store := New(pool)

	userID := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, status, kyc_level, created_at, updated_at) VALUES ($1, $2, 'hash', 'active', 'basic', now(), now())`, userID, fmt.Sprintf("user-%s@example.com", userID.String()))
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	created, err := store.CreateAPIKey(ctx, userID, "prefix123", "hash123", []string{"read"}, []string{})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	keys, err := store.ListAPIKeys(ctx, userID)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatalf("expected keys")
	}

	revoked, err := store.RevokeAPIKey(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("revoke api key: %v", err)
	}
	if !revoked {
		t.Fatalf("expected revoke true")
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
