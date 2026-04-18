# Architecture

## Package layout

```
fishhub-server/
├── main.go                  # entry point: wires everything together
├── db/
│   └── migrations/          # SQL migration files (golang-migrate format)
└── internal/
    ├── auth/                # Bearer token middleware, context helpers
    ├── db/                  # DB connection, migration runner, seed
    ├── handler/             # HTTP handlers (health, tokens, readings)
    ├── senml/               # SenML RFC 8428 parser
    ├── store/               # Data access layer (Postgres)
    └── testutil/            # Shared test helpers (test DB via testcontainers)
```

## Dependency graph

```
main.go
 ├── internal/db        — open connection, run migrations, seed user
 ├── internal/store     — TokenStore, DeviceStore (Postgres implementations)
 ├── internal/auth      — Authenticator middleware (depends on DeviceStore interface)
 └── internal/handler   — HTTP handlers (depend on store interfaces)
      └── internal/senml — SenML parsing (pure, no external deps)
```

`internal/senml` and `internal/testutil` have no dependencies on other internal packages.

## Design principles

**Dependency injection everywhere.** Handlers receive their stores as struct fields. No package-level singletons.

**Depend on interfaces, not concrete types.** Each handler declares the smallest interface it needs. This is why `TokensHandler` has a `TokenStore` field typed as the interface, not `*postgresTokenStore`. Same for `DeviceStore` in the auth middleware.

**Context-based auth.** The auth middleware stores `DeviceInfo` in the request context via `context.WithValue`. Downstream handlers retrieve it with `auth.DeviceFromContext()` — no need to re-query the DB.

**Transactional safety.** Token creation (device row + token row) is wrapped in a single transaction. A failure at any step rolls back cleanly.

## Request lifecycle

### POST /tokens
```
TokensHandler.Create
  └── TokenStore.CreateToken(userID)
        ├── crypto/rand → 32 bytes → 64-char hex token
        ├── INSERT INTO devices (user_id)
        └── INSERT INTO device_tokens (device_id, token)
  └── 201 {"token":"...","device_id":"...","user_id":"..."}
```

### POST /readings
```
Authenticator middleware
  ├── parse "Bearer <token>" from Authorization header
  ├── DeviceStore.LookupByToken(token)
  │     └── JOIN device_tokens + devices WHERE token = $1
  ├── 401 if missing or invalid
  └── store DeviceInfo in request context

ReadingsHandler.Create
  ├── auth.DeviceFromContext(ctx) → DeviceInfo
  ├── senml.Parse(body) → Reading{Temperature, BaseTime}
  ├── 400 on malformed payload
  ├── log reading (device_id, temperature, base_time)
  └── 201 {}
```

### GET /health
```
handler.Health → 200 {"status":"ok"}
```

## What's not yet implemented

- **InfluxDB persistence** — readings are logged to stdout but not stored (issue #4)
- **Grafana stack** — Docker Compose doesn't include InfluxDB/Grafana yet (issue #5)
- **Token security** — tokens stored as plaintext; hashing/JWT under evaluation (issue #6)
