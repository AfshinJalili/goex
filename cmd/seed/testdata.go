package main

import (
	"context"
	"time"

	"github.com/AfshinJalili/goex/libs/apikey"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	expiredKeyPrefix = "expired0001"
	expiredKeySecret = "expiredsecret0001"
)

func seedTestData(ctx context.Context, pool *pgxpool.Pool) error {
	mfaUserID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	suspendedUserID := uuid.MustParse("00000000-0000-0000-0000-000000000004")

	params := argon2Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 2,
		SaltLength:  16,
		KeyLength:   32,
	}

	mfaHash, err := hashPassword("mfa123", params)
	if err != nil {
		return err
	}

	suspendedHash, err := hashPassword("suspended123", params)
	if err != nil {
		return err
	}

	now := time.Now()

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, kyc_level, mfa_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (email) DO UPDATE SET mfa_enabled = $6
	`, mfaUserID, "mfa@example.com", mfaHash, "active", "basic", true, now, now)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, status, kyc_level, mfa_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (email) DO UPDATE SET status = $4
	`, suspendedUserID, "suspended@example.com", suspendedHash, "suspended", "basic", false, now, now)
	if err != nil {
		return err
	}

	expiredKeyID := uuid.MustParse("00000000-0000-0000-0000-000000000301")
	demoUserID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	hash := apikey.Hash(expiredKeyPrefix, expiredKeySecret)
	expiredTime := now.Add(-1 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO api_keys (id, user_id, prefix, key_hash, scopes, ip_whitelist, created_at, updated_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
		ON CONFLICT (prefix) DO UPDATE SET revoked_at = $9
	`, expiredKeyID, demoUserID, expiredKeyPrefix, hash, []string{"read"}, []string{}, now, now, expiredTime)
	if err != nil {
		return err
	}

	return nil
}
