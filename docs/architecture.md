# Architecture

## Package layout

```
fishhub-server/
├── main.go                  # entry point: wires everything together
├── railway.toml             # Railway deployment config (build + start commands, health check)
├── db/
│   └── migrations/          # SQL migration files (golang-migrate format)
└── internal/
    ├── sensors/             # domain: device tokens, readings ingestion + query, InfluxDB, SenML
    │   ├── handler.go               TokensHandler, ReadingsHandler, ReadingsQueryHandler,
    │   │                            DevicesHandler, ProvisionHandler, ActivateHandler,
    │   │                            PatchDeviceHandler
    │   ├── store.go                 DeviceStore, TokenStore, ProvisioningStore interfaces
    │   │                            + error sentinels (ErrCodeNotFound, ErrCodeAlreadyUsed,
    │   │                            ErrDeviceNotFound, ErrTokenNotFound)
    │   ├── store_postgres.go        DeviceStore + TokenStore Postgres impls (incl. PatchDevice)
    │   ├── store_provisioning_postgres.go  ProvisioningStore Postgres impl
    │   │                            (GetOrCreatePending, ClaimCode, Activate)
    │   ├── influx.go                InfluxClient (ReadingWriter + ReadingQuerier), influxDBClient
    │   ├── senml.go                 SenML RFC 8428 parser
    │   └── model.go                 DeviceInfo, TokenResult, Reading, context helpers
    ├── auth/                # domain: OIDC verification, JWT sessions, refresh token rotation
    │   ├── handler.go               VerifyHandler, RefreshHandler, LogoutHandler
    │   ├── service.go               AuthService interface, oidcService implementation
    │   ├── store.go                 UserStore + RefreshTokenStore interfaces
    │   ├── store_postgres.go        Postgres implementations
    │   └── model.go                 User, RefreshToken, error sentinels
    ├── account/             # domain: account profile (created on first login via event)
    │   ├── handler.go               MeHandler
    │   ├── store.go                 AccountStore interface
    │   ├── store_postgres.go        Postgres implementation
    │   ├── model.go                 Account struct
    │   └── events.go                AccountEventHandler (implements auth.UserEventHandler)
    ├── platform/            # cross-cutting: DB setup, middleware, health
    │   ├── db.go                    Open(), Migrate(), SeedUser(), SeedUserID()
    │   └── middleware.go            DeviceAuthenticator(), SessionAuthenticator(), Health()
    └── testutil/
        └── db.go                    NewTestDB(t) — starts Postgres container, runs migrations
```

## Dependency graph

```
main.go
 ├── platform     — DB connection, migrations, seed, middleware
 ├── sensors      — device tokens, readings ingestion + query (InfluxDB), SenML
 ├── auth         — OIDC verification, JWT + refresh token issuance
 └── account      — account profiles, MeHandler
      └── implements auth.UserEventHandler (called after OIDC verify)
```

## Design principles

**Dependency injection everywhere.** Handlers receive dependencies as struct fields — no package-level singletons.

**Depend on interfaces, not concrete types.** Each handler declares the smallest interface it needs (e.g. `TokenStore`, `DeviceStore`, `ReadingWriter`, `AuthService`). `main.go` is the only wiring point.

**Domain packages never import each other.** `sensors`, `auth`, and `account` are fully isolated. Cross-domain dependencies use interfaces (e.g. `auth.UserEventHandler` is satisfied by `account.AccountEventHandler`).

**`platform` may be imported by any domain.** It has no domain knowledge — only DB setup and middleware.

**Context-based auth.** Device auth middleware stores `DeviceInfo` in the request context. Session auth middleware stores `auth.Claims`. Downstream handlers retrieve them without re-querying the DB.

## Request lifecycle

### POST /tokens (no auth)
```
TokensHandler.Create
  └── TokenStore.CreateToken(userID)
        ├── crypto/rand → 32 bytes → 64-char hex token
        ├── INSERT INTO devices (user_id)
        └── INSERT INTO device_tokens (device_id, token)
  └── 201 {"token":"...","device_id":"...","user_id":"..."}
```

### POST /readings (device Bearer token)
```
DeviceAuthenticator middleware
  ├── parse "Bearer <token>" from Authorization header
  ├── DeviceStore.LookupByToken(token) → JOIN device_tokens + devices
  ├── 401 if missing/invalid
  └── store DeviceInfo{DeviceID, UserID} in context

ReadingsHandler.Create
  ├── DeviceFromContext(ctx)
  ├── ParseSenML(body) → Reading{Measurements, BaseTime}
  ├── 400 on malformed payload
  └── InfluxClient.WriteReading(ctx, Reading) → InfluxDB 3 Core
```

### GET /api/devices and /api/devices/{id}/readings (session JWT)
```
SessionAuthenticator middleware
  ├── read "Bearer <token>" header OR "session" cookie
  ├── AuthService.ValidateSessionJWT(token) → userID
  ├── 401 if missing/invalid
  └── store Claims{UserID} in context

DevicesHandler.List / ReadingsQueryHandler.List
  ├── ClaimsFromContext(ctx)
  ├── DeviceStore.ListByUserID (accepts optional ?status filter) / FindByIDAndUserID
  └── InfluxClient.QueryReadings (for readings endpoint)
```

### POST /api/devices/provision → device pairing (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

ProvisionHandler
  ├── ClaimsFromContext(ctx)
  └── ProvisioningStore.GetOrCreatePending(ctx, userID)
        ├── atomic upsert: INSERT INTO devices WHERE NOT EXISTS pending for user
        ├── INSERT INTO provisioning_codes (code CHAR(6), device_id) ON CONFLICT DO NOTHING
        └── return (code, device_id)
  └── 201 {"code":"...","device_id":"..."}
```

### POST /devices/activate → firmware token issuance (no auth)
```
ActivateHandler
  ├── decode body {code}
  ├── 400 if code missing
  ├── ProvisioningStore.ClaimCode(ctx, code)
  │     ├── UPDATE provisioning_codes SET used_at=now() WHERE code=? AND used_at IS NULL
  │     ├── 404 (ErrCodeNotFound) if no row matched
  │     └── 409 (ErrCodeAlreadyUsed) if row exists but used_at already set
  ├── crypto/rand → 32 bytes → 64-char hex Bearer token
  └── ProvisioningStore.Activate(ctx, deviceID, token)
        ├── INSERT INTO device_tokens (device_id, token)
        └── UPDATE devices SET status='active' WHERE id=deviceID
  └── 201 {"token":"...","device_id":"..."}
```

### PATCH /api/devices/{id} → rename device (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

PatchDeviceHandler
  ├── ClaimsFromContext(ctx)
  ├── decode body {name}; 400 if empty
  ├── DeviceStore.PatchDevice(ctx, deviceID, userID, name)
  │     ├── UPDATE devices SET name=? WHERE id=? AND user_id=?
  │     └── 404 (ErrDeviceNotFound) if no row matched
  └── 200 {"id":"...","name":"...","created_at":"..."}
```

### POST /auth/verify → session issuance
```
VerifyHandler
  ├── AuthService.VerifyAndUpsert(provider, idToken)
  │     ├── OIDC ID token verification (go-oidc)
  │     ├── UserStore.Upsert(email, provider, sub)
  │     └── UserEventHandler.OnUserVerified → AccountStore.Upsert
  ├── AuthService.IssueSessionJWT(userID) → signed HS256 JWT
  └── AuthService.IssueRefreshToken(ctx, userID) → stored hash, return raw
  └── 200 {"token":"<jwt>","refresh_token":"<64-char-hex>"}
```
