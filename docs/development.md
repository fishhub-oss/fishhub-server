# Development Guide

## Prerequisites

- Go 1.21+
- Docker (for Postgres, InfluxDB 3 Core, Grafana, Redis, EMQX, and integration tests)

## Running locally

```bash
cp .env.example .env   # first time only — fill in GOOGLE_CLIENT_ID and JWT keys
make dev
```

`make dev` starts Postgres, InfluxDB 3 Core, Grafana, Redis, and EMQX via Docker Compose, waits until all are healthy, then runs the Go server. The server listens on `:8080` by default.

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
| `MQTT_BROKER` | `emqx` | Broker selector: `emqx` (local Docker) or `hivemq` |
| `EMQX_HOST` | `localhost` | EMQX server hostname |
| `EMQX_PORT` | `1883` | EMQX plain-TCP MQTT port |
| `EMQX_SERVER_USERNAME` | — | MQTT credential for the server process |
| `EMQX_SERVER_PASSWORD` | — | MQTT credential for the server process |
| `EMQX_API_BASE_URL` | `http://localhost:18083` | EMQX REST API base URL |
| `EMQX_API_KEY` | `admin` | EMQX REST API key (dashboard default) |
| `EMQX_API_SECRET` | `public` | EMQX REST API secret (dashboard default) |
| `EMQX_AUTH_ID` | `password_based:built_in_database` | EMQX authentication resource ID |
| `EMQX_DEVICE_HOST` | `localhost` | MQTT hostname returned to firmware at activation |
| `EMQX_DEVICE_PORT` | `1883` | MQTT port returned to firmware at activation |

The Makefile sources `.env` automatically via `-include .env`.

If `INFLUXDB3_HOST`, `INFLUXDB3_TOKEN`, and `INFLUXDB3_DATABASE` are all set, the server connects to InfluxDB on startup. If any are missing, the server logs a warning and runs without InfluxDB — readings are accepted and logged but not persisted.

## First-run workflow

```bash
cp .env.example .env          # copy defaults; fill in GOOGLE_CLIENT_ID and JWT keys
make dev                      # start all services and run the server
```

In a second terminal, run the one-time setup steps:

```bash
make influx-setup             # create the InfluxDB database
```

For EMQX, you must first generate an API key in the dashboard before running setup:

1. Open http://localhost:18083 and log in (default: `admin` / `public`).
2. Go to **System → API Keys** and create a new key. Copy the key and secret — the secret is only shown once.
3. Update `EMQX_API_KEY` and `EMQX_API_SECRET` in your `.env` with the values from step 2.
4. Run:

```bash
make emqx-setup               # create auth backend + seed the server MQTT credential
```

```bash
curl -s localhost:8080/health  # verify server is up
```

`make influx-setup` and `make emqx-setup` are one-time setup steps. Docker volumes persist data across restarts, so they do not need to be re-run unless you wipe the volumes.

## InfluxDB setup

After `make dev`, create the InfluxDB database (first time only):

```bash
make influx-setup
```

This runs `influxdb3 create database` inside the InfluxDB container using the configured token and database name from `.env`.

## EMQX setup

After `make dev`, run the following once to configure EMQX for local development.

**Step 1 — generate an API key**

The EMQX default credentials (`admin` / `public`) are for the dashboard UI only and cannot be used directly as API credentials. You must create a dedicated API key first:

1. Open http://localhost:18083 and log in (`admin` / `public`).
2. Go to **System → API Keys** and create a new key.
3. Copy the key ID and secret — **the secret is only shown once**.
4. Set `EMQX_API_KEY` and `EMQX_API_SECRET` in your `.env` to these values.

**Step 2 — run setup**

```bash
make emqx-setup
```

This does two things via the EMQX REST API:
1. Creates the `password_based:built_in_database` authentication backend.
2. Creates the server MQTT user (`EMQX_SERVER_USERNAME` / `EMQX_SERVER_PASSWORD`).

Both steps are idempotent — safe to re-run if something fails partway through.

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
