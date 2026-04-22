# fishhub-server — Documentation Index

FishHub backend server: receives temperature readings from ESP32 devices, authenticates them via Bearer tokens, stores data in InfluxDB, and serves a JSON API for the web frontend.

## Contents

| Document | What it covers |
|---|---|
| [architecture.md](architecture.md) | Package layout, data flow, design principles |
| [api.md](api.md) | HTTP endpoints, request/response formats |
| [database.md](database.md) | Schema, migrations, seed data |
| [auth.md](auth.md) | Device token lifecycle, OIDC/JWT session auth, middleware |
| [development.md](development.md) | Running locally, environment variables, testing |

## Quick start

```bash
make dev                          # start Postgres + InfluxDB + Grafana, then run server
curl -s -X POST localhost:8080/tokens | jq   # get a device token
curl -s localhost:8080/health                # verify server is up
```

## Tech stack

- **Language:** Go
- **Router:** chi v5
- **Database:** PostgreSQL (application data) · InfluxDB 3 Core (time-series readings)
- **Migrations:** golang-migrate
- **Auth:** Google OIDC → JWT session + refresh token rotation; device Bearer tokens
- **Tests:** unit (stubs) + integration (testcontainers)
