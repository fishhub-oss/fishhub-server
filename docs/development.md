# Development Guide

## Prerequisites

- Go 1.21+
- Docker (for Postgres, InfluxDB 3 Core, Grafana, and integration tests)

## Running locally

```bash
make dev
```

This starts Postgres, InfluxDB 3 Core, and Grafana via Docker Compose, waits until all are healthy, then runs the server. The server listens on `:8080` by default.

The Makefile also prints the machine's local IP addresses (useful for configuring the firmware's `SERVER_URL`).

To use a different port:
```bash
PORT=9090 make dev
```

## Environment variables

| Variable | Default (Makefile / `.env`) | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable` | Postgres connection string |
| `PORT` | `8080` | HTTP listen port |
| `INFLUXDB3_HOST` | — | InfluxDB 3 Core host URL (e.g. `http://localhost:8086`) |
| `INFLUXDB3_TOKEN` | — | InfluxDB admin token |
| `INFLUXDB3_DATABASE` | — | InfluxDB database name |
| `GOOGLE_CLIENT_ID` | — | Google OAuth client ID (OIDC verification) |
| `SESSION_JWT_PRIVATE_KEY` | — | PEM-encoded RSA private key for signing RS256 session JWTs (`\n`-escaped for env) |
| `SESSION_JWT_KID` | — | Key ID included in the JWT header and JWKS entry (e.g. `session-v1`) |
| `JWT_TTL_HOURS` | `24` | Session JWT validity in hours |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3001` | Comma-separated list of allowed CORS origins |

The Makefile sources `.env` automatically via `-include .env`.

If `INFLUXDB3_HOST`, `INFLUXDB3_TOKEN`, and `INFLUXDB3_DATABASE` are all set, the server connects to InfluxDB on startup. If any are missing, the server logs a warning and runs without InfluxDB — readings are accepted and logged but not persisted.

## First-run workflow

```bash
make dev                                      # start everything

curl -s -X POST localhost:8080/tokens | jq    # get a device token
# copy "token" into firmware's config.h as DEVICE_TOKEN
# copy the printed IP into config.h as SERVER_URL

curl -s localhost:8080/health                 # verify server is up
```

## InfluxDB setup

After `make dev`, create the InfluxDB database (first time only):

```bash
make influx-setup
```

This runs `influxdb3 create database` inside the InfluxDB container using the configured token and database name from `.env`.

## Testing

**Unit tests** (stubs, no Docker required):
```bash
go test ./internal/auth/... ./internal/sensors/... ./internal/platform/...
```

**Integration tests** (spin up a real Postgres container via testcontainers, Docker required):
```bash
go test ./internal/account/... ./internal/auth/... ./internal/sensors/...
```

**All tests:**
```bash
go test ./...
```

Integration tests use `testutil.NewTestDB(t)` — each test gets a throwaway Postgres container with a clean schema. No shared state between tests.

## Adding a new migration

1. Create `db/migrations/NNN_<description>.up.sql` and `NNN_<description>.down.sql`
2. Migrations run automatically on the next server startup

## Project conventions

- All dependencies injected via struct fields — no package-level state
- Handlers depend on interfaces, not concrete store types
- Domain packages (`sensors`, `auth`, `account`) never import each other
- Integration tests use `testutil.NewTestDB(t)` — never mock the database
- See [architecture.md](architecture.md) for the full package picture
