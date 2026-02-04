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

## Notes
- Defaults match the local docker-compose stack.
- Use an existing Secret in production instead of inline passwords.
