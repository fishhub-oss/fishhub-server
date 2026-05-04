# fishhub-server

Go backend for FishHub — an open-source aquarium monitoring and automation platform. It receives sensor readings from ESP32 devices over MQTT and a REST API, authenticates devices with RS256-signed JWTs, stores application state in PostgreSQL, and writes time-series data to InfluxDB 3 Core for Grafana dashboards. A transactional outbox handles async side-effects such as HiveMQ credential provisioning and MQTT config pushes.

## Architecture

```
┌─────────────┐   MQTT (TLS)   ┌──────────────────────────────────────────┐
│  ESP32 MCU  │───────────────▶│           fishhub-server (Go)            │
│  (firmware) │◀───────────────│                                          │
└─────────────┘   commands     │  REST API (chi v5) · MQTT subscriber     │
                               └──────┬───────────────────────┬───────────┘
                                      │                       │
                           ┌──────────▼──────────┐  ┌────────▼──────────┐
                           │    PostgreSQL 16     │  │  InfluxDB 3 Core  │
                           │  users · devices     │  │  time-series      │
                           │  tokens · outbox     │  │  sensor readings  │
                           └─────────────────────┘  └────────┬──────────┘
                                                             │
                                                   ┌─────────▼──────────┐
                                                   │       Grafana       │
                                                   └────────────────────┘
```

**MQTT / HiveMQ Cloud** — devices publish readings to `fishhub/{device_id}/readings` and subscribe to command topics. The server provisions per-device MQTT credentials via the HiveMQ Cloud REST API (using a transactional outbox for reliability) and subscribes to device topic trees to ingest readings server-side.

**Package layout** (`internal/`):

| Package | Responsibility |
|---|---|
| `device` | Device lifecycle, rename, soft-delete, MQTT credential revocation |
| `provisioning` | Pairing codes, device activation, HiveMQ outbox processor |
| `measurement` | SenML ingestion, InfluxDB reads/writes, MQTT readings subscription |
| `peripheral` | Peripheral CRUD, schedule management, control-mode, command publish |
| `account` | Account profile, timezone, config-push outbox processor |
| `auth` | Google OIDC verification, session JWT + refresh token rotation |
| `mqtt` | Publisher and subscriber wrappers (TLS, HiveMQ Cloud) |
| `hivemq` | HiveMQ Cloud REST API client (provision / delete credentials) |
| `outbox` | Transactional outbox runner for async side-effects |
| `jwtutil` | Low-level RSA JWT signing + JWKS endpoint |
| `devicejwt` | Device JWT issuance (RS256, 10-year TTL) |
| `platform` | DB setup, migrations, request middleware |
| `api` | HTTP handler wiring |

All optional infrastructure components (`mqtt`, `hivemq`, `devicejwt`, `jwtutil`) provide `NewNoOp()` implementations so the server starts cleanly without external services configured.

Full design details: [`docs/architecture.md`](docs/architecture.md).

## Prerequisites

- **Go 1.25+**
- **Docker** — local services (Postgres, InfluxDB 3 Core, Grafana) and integration tests (testcontainers)
- **make**

## Local development

### 1. Configure environment

Create a `.env` file in the repo root (sourced automatically by the Makefile):

```bash
# Postgres (matches docker-compose.yml defaults)
DATABASE_URL=postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable

# InfluxDB 3 Core
INFLUXDB3_HOST=http://localhost:8181
INFLUXDB3_TOKEN=<your-admin-token>
INFLUXDB3_DATABASE=fishhub

# Required by docker-compose to mount the InfluxDB admin token
INFLUX_TOKEN_FILE=/path/to/.influxdb-admin-token.json
```

All other variables (auth, HiveMQ, CORS) have safe defaults or no-op fallbacks — the server starts without them.

### 2. Start services and run the server

```bash
make dev
```

This starts Postgres, InfluxDB 3 Core, and Grafana via Docker Compose, waits for all three to be healthy, prints local IP addresses (useful when flashing firmware with `SERVER_URL`), then runs the server on `:8080`.

### 3. First-time InfluxDB database setup

```bash
make influx-setup
```

Creates the InfluxDB database. Required once per fresh data directory.

### Run tests

```bash
go test ./...         # all (Docker required for integration tests)
go test -short ./...  # unit tests only, no Docker
```

Integration tests use testcontainers — each test gets a throwaway Postgres container with a clean schema.

## Configuration

The server reads all configuration from environment variables. The Makefile sources `.env` automatically.

### Core

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `DATABASE_URL` | — | Postgres connection string |
| `LOG_FORMAT` | text | Set to `json` for structured logs (Railway / production) |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:3001` | Comma-separated list of allowed CORS origins |

### InfluxDB

| Variable | Description |
|---|---|
| `INFLUXDB3_HOST` | InfluxDB 3 Core host URL (e.g. `http://localhost:8181`) |
| `INFLUXDB3_TOKEN` | Admin token |
| `INFLUXDB3_DATABASE` | Database name |

If any of these are unset the server starts in degraded mode — readings are accepted but not persisted.

### Authentication

| Variable | Description |
|---|---|
| `GOOGLE_CLIENT_ID` | Google OAuth client ID (OIDC ID token verification) |
| `SESSION_JWT_PRIVATE_KEY` | PEM-encoded RSA private key for session JWTs (`\n`-escaped) |
| `SESSION_JWT_KID` | Key ID for the session JWT (e.g. `session-v1`) |
| `JWT_TTL_HOURS` | Session JWT lifetime in hours (default: `24`) |
| `DEVICE_JWT_PRIVATE_KEY` | PEM-encoded RSA private key for device JWTs (`\n`-escaped) |
| `DEVICE_JWT_KID` | Key ID for device JWTs |
| `IDP_HOST` | Issuer (`iss`) claim in device JWTs (e.g. `https://api.fishhub.io`) |

RSA private keys can be stored as PEM files in a git-ignored `secrets/` directory; the Makefile loads `secrets/device_jwt_private_key.pem` automatically.

### HiveMQ Cloud

| Variable | Default | Description |
|---|---|---|
| `HIVEMQ_API_BASE_URL` | — | HiveMQ Cloud REST API base URL |
| `HIVEMQ_API_TOKEN` | — | HiveMQ Cloud API token |
| `HIVEMQ_DEVICE_ROLE_ID` | — | Role ID attached to provisioned device credentials |
| `HIVEMQ_HOST` | — | MQTT broker hostname |
| `HIVEMQ_PORT` | `8883` | MQTT broker TLS port |
| `HIVEMQ_SERVER_USERNAME` | — | Server MQTT username (publish + subscribe) |
| `HIVEMQ_SERVER_PASSWORD` | — | Server MQTT password |

All HiveMQ variables are optional. Without them the server runs with no-op MQTT publisher/subscriber — suitable for local development without a broker.

### Docker Compose secrets

`docker-compose.yml` mounts the InfluxDB admin token from a local file. Set `INFLUX_TOKEN_FILE` in `.env` to the absolute path of a JSON file containing `{"token":"<value>","name":"admin"}`. `make dev` creates this file automatically from `INFLUXDB3_TOKEN`.

## API

All endpoints are documented in [`docs/api.md`](docs/api.md). Quick reference:

| Group | Endpoints |
|---|---|
| Health | `GET /health` |
| Auth | `POST /auth/verify` · `POST /auth/refresh` · `POST /auth/logout` |
| Account | `GET /api/me` · `PATCH /api/me` |
| Device provisioning | `POST /api/devices/provision` · `POST /devices/activate` · `GET /devices/{id}/status` |
| Devices | `GET /api/devices` · `PATCH /api/devices/{id}` · `DELETE /api/devices/{id}` |
| Readings | `GET /api/devices/{id}/readings` |
| Peripherals | `POST /api/devices/{id}/peripherals` · `GET /api/devices/{id}/peripherals` · `PUT …/{peripheralId}/schedule` · `PATCH …/{peripheralId}/control-mode` · `DELETE …/{peripheralId}` |
| Actuator commands | `POST /api/devices/{id}/peripherals/{peripheralId}/commands` |
| JWKS | `GET /.well-known/jwks.json` |

## Deployment

The server is deployed on [Railway](https://railway.app) using a multi-stage Dockerfile. The build stage compiles the Go binary; the runtime image is Alpine with the binary and `db/migrations/` copied in. Migrations run automatically on every server startup via `platform.Migrate`.

`railway.toml` configures the start command and health-check path (`/health`).

> **Known issue:** See [#95](https://github.com/fishhub-oss/fishhub-server/issues/95) for a tracked deployment issue.

## License

[MIT](LICENSE)
