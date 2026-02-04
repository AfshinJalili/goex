# Database Migrations

This project uses the `golang-migrate` CLI.

## Install
- https://github.com/golang-migrate/migrate

## Run Migrations (Local)
```bash
POSTGRES_HOST=localhost \
POSTGRES_PORT=5432 \
POSTGRES_DB=cex_core \
POSTGRES_USER=cex \
POSTGRES_PASSWORD=cex \
./scripts/migrate-up.sh
```

## Rollback Last Migration
```bash
./scripts/migrate-down.sh
```

## Create New Migration
```bash
./scripts/migrate-new.sh add_new_table
```

## Migration History

### Phase 1: Auth & User Services (0001-0008)
- 0001: Enable UUID extension
- 0002: Create users table
- 0003: Create accounts table
- 0004: Create API keys table
- 0005: Create audit logs table
- 0006: Create refresh tokens table
- 0007: Add accounts status column
- 0008: Create ledger accounts table

### Phase 2: Trading Core (0009-0013)
- 0009: Create assets and markets tables
- 0010: Create orders table
- 0011: Create trades table
- 0012: Create ledger entries table
- 0013: Create fee tiers table

These migrations add trading core tables required for Phase 2 services.

## Notes
- Defaults match the local docker-compose stack.
- Use an existing Secret in production instead of inline passwords.
