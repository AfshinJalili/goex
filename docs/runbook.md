# Operations Runbook

## Start/Stop
- Start: `make dev-start`
- Stop: `make dev-stop`
- Logs: `make dev-logs`

## Health Checks
Run service checks:
```bash
./scripts/verify-services.sh
```

## Kafka DLQ
Failed messages are published to the `dead_letter` topic when marked non-retriable.
- Inspect: `kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic dead_letter`

## Database Maintenance
- Migrate up: `./scripts/migrate-up.sh`
- Migrate down: `./scripts/migrate-down.sh`

## Incident Checklist
1. Check service health endpoints.
2. Review logs for error spikes.
3. Verify Kafka consumer lag.
4. Inspect `dead_letter` for poison messages.
5. Validate database connectivity and locks.
