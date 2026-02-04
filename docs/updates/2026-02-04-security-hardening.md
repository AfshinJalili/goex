# Security + Reliability Hardening Update - 2026-02-04

## Summary
Strengthened auth reliability and rate limiting, enforced explicit JWT secrets, added login payload validation, and made logout idempotent. Updated tests to cover these behaviors.

## Decisions
- `CEX_JWT_SECRET` must be explicitly set; no default secret allowed.
- Login payloads with blank email/password return `400 INVALID_REQUEST`.
- Logout is idempotent for valid payloads.
- Rate limiting uses Redis when configured, with a dev/test memory fallback.
- `Retry-After` header is returned on rate-limited responses.

## Fixes Applied
- Enforced explicit JWT secrets in auth/user configs.
- Added Redis-backed rate limiter with a clean interface.
- Added input validation for login payloads (trim + non-empty).
- Made logout idempotent.

## Tests Added
- Memory limiter allow/reset behavior.
- Login validation edge cases and concurrency.

## Tests Run
- `go test ./...`

## Files Touched
- `services/auth/internal/config/config.go`
- `services/user/internal/config/config.go`
- `services/auth/internal/handlers/auth.go`
- `services/auth/internal/handlers/auth_test.go`
- `services/auth/internal/rate/limiter.go`
- `services/auth/internal/rate/memory.go`
- `services/auth/internal/rate/redis.go`
- `docs/updates/2026-02-04-security-hardening.md`
