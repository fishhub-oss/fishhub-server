# fishhub-server — Documentation Index

FishHub backend server: receives temperature readings from ESP32 devices, authenticates them via Bearer tokens, and stores data for visualization.

## Contents

| Document | What it covers |
|---|---|
| [architecture.md](architecture.md) | Package layout, data flow, design principles |
| [api.md](api.md) | HTTP endpoints, request/response formats |
| [database.md](database.md) | Schema, migrations, seed data |
| [auth.md](auth.md) | Token lifecycle, auth middleware |
| [development.md](development.md) | Running locally, environment variables, testing |

## Quick start

```bash
make dev                          # start Postgres + run server
curl -s -X POST localhost:8080/tokens | jq   # get a device token
curl -s localhost:8080/health                # verify server is up
```

## Tech stack

- **Language:** Go
- **Router:** chi v5
- **Database:** PostgreSQL (application data) · InfluxDB (metrics, upcoming)
- **Migrations:** golang-migrate
- **Tests:** unit (stubs) + integration (testcontainers)
