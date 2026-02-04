# User Service

Provides authenticated user/account/balance endpoints:
- `GET /me`
- `GET /accounts`
- `GET /balances`

## Configuration
Requires `CEX_JWT_SECRET` (explicitly set) and Postgres connection env vars:
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`

## Manual Test
1. Start Postgres (see `deploy/docker-compose.yml`).
2. Apply migrations: `./scripts/migrate-up.sh`.
3. Run the service:
   ```bash
   go run ./services/user/cmd/user
   ```
4. Call `/me`, `/accounts`, `/balances` with `Authorization: Bearer <jwt>`.

## Notes
- `/balances` expects `ledger_accounts` to be populated.
