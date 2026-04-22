# Architecture

## Package layout

```
fishhub-server/
├── main.go                  # entry point: wires everything together
├── db/
│   └── migrations/          # SQL migration files (golang-migrate format)
└── internal/
    ├── sensors/             # domain: device tokens, readings ingestion + query, InfluxDB, SenML
    │   ├── handler.go               TokensHandler, ReadingsHandler, ReadingsQueryHandler, DevicesHandler
    │   ├── store.go                 DeviceStore + TokenStore interfaces
    │   ├── store_postgres.go        Postgres implementations
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
  ├── DeviceStore.ListByUserID / FindByIDAndUserID
  └── InfluxClient.QueryReadings (for readings endpoint)
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
```
