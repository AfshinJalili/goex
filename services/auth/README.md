# Auth Service

This service provides authentication endpoints for login, token refresh, and logout.

## Endpoints
- `POST /auth/login`
- `POST /auth/refresh`
- `POST /auth/logout`

## Configuration
The service uses the shared `config.yaml` format and environment overrides.

Required env vars:
- `CEX_JWT_SECRET` (must be set explicitly)
- `POSTGRES_HOST`
- `POSTGRES_PORT`
- `POSTGRES_DB`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`

Optional env vars:
- `CEX_ACCESS_TOKEN_TTL` (default: `15m`)
- `CEX_REFRESH_TOKEN_TTL` (default: `720h`)
- `CEX_JWT_ISSUER` (default: `cex-auth`)
- `CEX_ARGON2_MEMORY` (default: `65536`)
- `CEX_ARGON2_ITERATIONS` (default: `3`)
- `CEX_ARGON2_PARALLELISM` (default: `2`)
- `CEX_ARGON2_SALT_LENGTH` (default: `16`)
- `CEX_ARGON2_KEY_LENGTH` (default: `32`)
- `CEX_LOGIN_RATE_LIMIT` (default: `10`)
- `CEX_LOGIN_RATE_WINDOW` (default: `1m`)
- `CEX_RATE_LIMIT_REDIS_ADDR` (optional; if set, Redis-backed limiter is used)
- `CEX_RATE_LIMIT_REDIS_PASSWORD`
- `CEX_RATE_LIMIT_REDIS_DB`
- `CEX_RATE_LIMIT_REDIS_PREFIX` (default: `cex:auth:rl:`)

## Manual Test
1. Start Postgres (see `deploy/docker-compose.yml`).
2. Apply migrations: `./scripts/migrate-up.sh`.
3. Run the service:
   ```bash
   go run ./services/auth/cmd/auth
   ```
4. Create a user in Postgres with a valid `password_hash` (argon2id encoded).
5. Call `/auth/login` with email/password to receive tokens.

## Notes
- MFA is a stub: if `mfa_enabled=true`, any non-empty `mfa_code` is accepted.
- Refresh tokens are rotated on refresh. Reuse triggers session revocation.
