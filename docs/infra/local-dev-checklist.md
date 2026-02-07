# Local Development Verification Checklist

1. Prerequisites check (Docker, Go, migrate CLI)
2. Start infrastructure: `./scripts/dev-up.sh`
3. Verify infrastructure: Check each service in `docker compose ps`
4. Verify migrations: Check tables exist in Postgres
5. Verify seed data: Query users table, check demo user exists
6. Verify services: Check health endpoints (Auth/User/Fee HTTP, Fee gRPC)
7. Verify gateway: Test Kong admin API
8. Run integration tests: `make dev-test`
9. Manual API testing: Example curl commands for login, get user, etc.

## Fee Service Checks

Fee HTTP health:
```bash
curl -s http://localhost:8082/healthz
```

Fee gRPC health:
```bash
grpc_health_probe -addr=localhost:9090
```

## Manual API Examples

Login:
```bash
curl -s -X POST http://localhost:8000/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"demo@example.com","password":"demo123"}'
```

Get user:
```bash
curl -s http://localhost:8000/me \
  -H "Authorization: Bearer <token>"
```

Get accounts:
```bash
curl -s http://localhost:8000/accounts \
  -H "Authorization: Bearer <token>"
```

Get balances:
```bash
curl -s http://localhost:8000/balances \
  -H "Authorization: Bearer <token>"
```
