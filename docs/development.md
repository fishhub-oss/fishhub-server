# Development Guide

## Prerequisites

- Go 1.21+
- Docker (for Postgres and integration tests)

## Running locally

```bash
make dev
```

This starts Postgres via Docker Compose, waits until it's ready, then runs the server. The server listens on `:8080` by default.

To use a different port:
```bash
PORT=9090 make dev
```

To use a different database:
```bash
make dev DATABASE_URL=postgres://user:pass@host/db?sslmode=disable
```

## Environment variables

| Variable | Default (Makefile) | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable` | Postgres connection string |
| `PORT` | `8080` | HTTP listen port |

InfluxDB env vars will be added when issue #4 is implemented.

## Typical first-run workflow

```bash
make dev                                      # start everything

curl -s -X POST localhost:8080/tokens | jq    # get a token
# copy "token" value into firmware's config.h as DEVICE_TOKEN
# copy your machine's local IP into config.h as SERVER_URL

curl -s localhost:8080/health                 # verify server is up
```

## Testing

**Unit tests** — use stubs, no Docker required:
```bash
go test ./internal/handler/... ./internal/senml/... ./internal/auth/...
```

**Integration tests** — spin up a real Postgres container via testcontainers:
```bash
go test ./internal/store/...
```

**All tests:**
```bash
go test ./...
```

Integration tests require Docker to be running. Each test gets its own throwaway container with a clean schema — no shared state between tests.

## Adding a new migration

1. Create `db/migrations/NNN_<description>.up.sql` and `NNN_<description>.down.sql`
2. Migrations run automatically on the next `make dev` / server startup

## Project conventions

- All dependencies injected via struct fields — no package-level state
- Handlers depend on interfaces, not concrete store types
- New stores go in `internal/store/`, new handlers in `internal/handler/`
- Integration tests use `testutil.NewTestDB(t)` to get an isolated DB
- See [architecture.md](architecture.md) for the full picture
