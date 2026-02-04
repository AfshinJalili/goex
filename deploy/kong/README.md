# Kong Gateway (DB-less)

This project uses a DB-less Kong configuration for Phase 1. The declarative config lives at `deploy/kong/kong.yaml`.

## Run Kong (DB-less)
Set the config path and JWT secret, then start Kong:

```bash
export KONG_DECLARATIVE_CONFIG=$(pwd)/deploy/kong/kong.yaml
export JWT_SECRET=change-me

# Example (Docker)
# docker run --rm -e KONG_DATABASE=off \
#   -e KONG_DECLARATIVE_CONFIG=/kong/kong.yaml \
#   -e JWT_SECRET=$JWT_SECRET \
#   -v $KONG_DECLARATIVE_CONFIG:/kong/kong.yaml \
#   -p 8000:8000 -p 8001:8001 kong:3.6
```

## Required Env Vars
- `KONG_DECLARATIVE_CONFIG`
- `JWT_SECRET`

## Smoke Tests
- `/me` without JWT -> 401
- `/auth/login` rate limits after 10/min
- `/orders` requires `X-API-Key` (placeholder policy)

## Notes
- JWT issuer must be `cex-auth` (see `CEX_JWT_ISSUER`).
- API key verification is a placeholder; Kong does not yet sync keys from the database.
