# Architecture

## Package layout

```
fishhub-server/
├── main.go                  # entry point: wires everything together
├── railway.toml             # Railway deployment config (build + start commands, health check)
├── db/
│   └── migrations/          # SQL migration files (golang-migrate format)
└── internal/
    ├── sensors/             # domain: device provisioning, readings ingestion + query, commands
    │   ├── handler.go               ReadingsHandler, ReadingsQueryHandler,
    │   │                            DevicesHandler, DeleteDeviceHandler, PatchDeviceHandler,
    │   │                            ProvisionHandler, ActivateHandler, ActivationStatusHandler,
    │   │                            CommandHandler
    │   ├── service.go               DeviceService, ReadingsService, ProvisioningService,
    │   │                            ActivationService, HiveMQProvisionProcessor
    │   ├── store.go                 DeviceStore, ProvisioningStore interfaces
    │   │                            + error sentinels (ErrCodeNotFound, ErrCodeAlreadyUsed,
    │   │                            ErrDeviceNotFound, ErrInvalidCommand, ErrInfluxWrite)
    │   ├── store_postgres.go        DeviceStore Postgres impl
    │   ├── store_provisioning_postgres.go  ProvisioningStore Postgres impl
    │   ├── influx.go                InfluxClient (ReadingWriter + ReadingQuerier), influxDBClient
    │   ├── senml.go                 SenML RFC 8428 parser
    │   └── model.go                 DeviceInfo, context helpers
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
    ├── devicejwt/           # device JWT issuance — wraps jwtutil with device-specific claims
    │   └── devicejwt.go             Signer interface (Sign, PublicKey, KID)
    │                                deviceSigner (RS256, claims: sub, user_id, iss, iat, exp)
    │                                NewNoOp() for unconfigured environments
    ├── jwtutil/             # low-level JWT primitives and JWKS endpoint
    │   ├── signer.go                Signer interface (Sign, PublicKey, KID)
    │   │                            rsaSigner — PKCS1 and PKCS8 PEM key support
    │   │                            NewNoOp() for unconfigured environments
    │   └── jwks.go                  JWKSHandler — serves GET /.well-known/jwks.json
    │                                aggregates public keys from one or more Signers
    ├── mqtt/                # MQTT command publishing
    │   └── publisher.go             Publisher interface (Publish)
    │                                pahoPublisher — TLS connection to HiveMQ broker (QoS 1)
    │                                NewNoOpPublisher() when HIVEMQ_HOST not set
    ├── hivemq/              # HiveMQ Cloud REST API client
    │   └── client.go                Client interface (ProvisionDevice, DeleteDevice)
    │                                apiClient — creates/deletes MQTT credentials + attaches role
    │                                NewNoOp() when HIVEMQ_API_BASE_URL not set
    ├── outbox/              # transactional outbox for async side-effects
    │   ├── outbox.go                Event, EventProcessor interface, Store interface
    │   ├── runner.go                Runner — polls Store, dispatches to EventProcessors
    │   │                            one goroutine per event type per tick; sequential within type
    │   └── store_postgres.go        Store Postgres impl (ClaimBatch with SELECT FOR UPDATE SKIP LOCKED)
    └── testutil/
        └── db.go                    NewTestDB(t) — starts Postgres container, runs migrations
```

## Dependency graph

```
main.go
 ├── platform     — DB connection, migrations, seed, middleware
 ├── jwtutil      — low-level JWT signing + JWKS handler
 ├── devicejwt    — device JWT issuance (wraps jwtutil)
 ├── sensors      — device provisioning, readings ingestion + query, commands
 │    └── uses outbox.Store (for activation), hivemq.Client (for delete), mqtt.Publisher (for commands)
 ├── auth         — OIDC verification, JWT + refresh token issuance
 ├── account      — account profiles, MeHandler
 │    └── implements auth.UserEventHandler (called after OIDC verify)
 ├── hivemq       — HiveMQ Cloud REST API client
 ├── mqtt         — MQTT broker publisher
 └── outbox       — outbox runner + Postgres store
      └── runs sensors.HiveMQProvisionProcessor as EventProcessor
```

## Design principles

**Dependency injection everywhere.** Handlers and services receive dependencies as struct fields — no package-level singletons.

**Depend on interfaces, not concrete types.** Each handler declares the smallest interface it needs (e.g. `DeviceStore`, `ProvisioningStore`, `CommandPublisher`). `main.go` is the only wiring point.

**Domain packages never import each other.** `sensors`, `auth`, and `account` are fully isolated. Cross-domain dependencies use interfaces.

**`platform` may be imported by any domain.** It has no domain knowledge — only DB setup and middleware.

**Context-based auth.** Device auth middleware stores `DeviceInfo` in the request context. Session auth middleware stores `auth.Claims`. Downstream handlers retrieve them without re-querying the DB.

**Noop implementations for optional infrastructure.** `devicejwt`, `jwtutil`, `mqtt`, and `hivemq` all provide a `NewNoOp()` that satisfies the interface but does nothing. This lets the server start without external services configured (e.g. in development without HiveMQ credentials).

## Request lifecycle

### POST /readings (device JWT auth)
```
DeviceAuthenticator middleware
  ├── parse "Bearer <token>" from Authorization header
  ├── validate RS256 JWT signature with devicejwt.Signer.PublicKey()
  ├── extract sub (device_id) and user_id claims
  ├── 401 if missing/invalid
  └── store DeviceInfo{DeviceID, UserID} in context

ReadingsHandler.Create
  ├── DeviceFromContext(ctx)
  ├── ParseSenML(body) → Reading{Measurements, BaseTime}
  ├── 400 on malformed payload
  └── InfluxClient.WriteReading(ctx, Reading) → InfluxDB 3 Core
```

### POST /api/devices/provision → device pairing (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

ProvisionHandler
  ├── ClaimsFromContext(ctx)
  └── ProvisioningStore.GetOrCreateCode(ctx, userID)
        ├── upsert provisioning_codes row for the user (code CHAR(6))
        └── return code
  └── 201 {"code":"..."}
```

### POST /devices/activate → device creation + JWT issuance (no auth)
```
ActivateHandler
  ├── decode body {code}; 400 if missing
  ├── ProvisioningStore.ClaimCode(ctx, code)
  │     ├── UPDATE provisioning_codes SET used_at=now() WHERE code=? AND used_at IS NULL
  │     ├── INSERT INTO devices (user_id) → deviceID
  │     ├── 404 (ErrCodeNotFound) if no row matched
  │     └── 409 (ErrCodeAlreadyUsed) if used_at already set
  ├── devicejwt.Signer.Sign(deviceID, userID) → RS256 JWT
  └── ActivationService.activate (within DB transaction):
        ├── outbox.Store.Insert(tx, "hivemq.provision", payload)
        └── commit
  └── 202 {"token":"<jwt>","device_id":"..."}
```

### Async MQTT provisioning (outbox runner)
```
outbox.Runner (background goroutine, polls every 10s)
  ├── outbox.Store.ClaimBatch() → []Event (SELECT FOR UPDATE SKIP LOCKED)
  └── for each "hivemq.provision" event:
        sensors.HiveMQProvisionProcessor.Process(ctx, event)
          ├── hivemq.Client.ProvisionDevice(username, password)
          │     ├── POST /mqtt/credentials → create credential
          │     └── PUT /user/{username}/roles/{roleID}/attach
          └── DeviceStore writes mqtt_username + mqtt_password to devices row
        outbox.Store.MarkCompleted(id)
        (on failure: RecordFailure; after 5 attempts: status='dead')
```

### GET /devices/{id}/status → activation polling (device JWT auth)
```
DeviceAuthenticator middleware
  └── store DeviceInfo{DeviceID, UserID} in context

ActivationStatusHandler
  ├── verify JWT sub == {id} path param; 403 if mismatch
  └── DeviceStore.GetActivationStatus(deviceID)
        └── ready = mqtt_username + mqtt_password present AND no pending/processing outbox event
  ├── not ready → 200 {"status":"provisioning"}
  └── ready     → 200 {"status":"ready","mqtt_username":"...","mqtt_password":"...","mqtt_host":"...","mqtt_port":8883}
```

### POST /api/devices/{id}/peripherals/{name}/commands → MQTT command (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

CommandHandler
  ├── ClaimsFromContext(ctx)
  ├── read raw JSON body
  └── DeviceService.SendCommand(ctx, deviceID, userID, peripheralName, body)
        ├── validate action field ("set" | "schedule"); 400 (ErrInvalidCommand) otherwise
        ├── DeviceStore.FindByIDAndUserID → 404 if not found
        └── mqtt.Publisher.Publish(topic="fishhub/{deviceID}/{peripheralName}/commands", payload)
  └── 204
```

### DELETE /api/devices/{id} → soft-delete + MQTT credential revocation (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

DeleteDeviceHandler
  ├── ClaimsFromContext(ctx)
  └── DeviceService.Delete(ctx, deviceID, userID)
        ├── DeviceStore.DeleteDevice(deviceID, userID)
        │     ├── UPDATE devices SET deleted_at=now() WHERE id=? AND user_id=?
        │     └── return mqtt_username for cleanup
        └── hivemq.Client.DeleteDevice(mqttUsername) — best effort
  └── 204
```

### GET /api/devices and /api/devices/{id}/readings (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

DevicesHandler.List / ReadingsQueryHandler.List
  ├── ClaimsFromContext(ctx)
  ├── DeviceStore.ListByUserID / FindByIDAndUserID
  └── InfluxClient.QueryReadings (for readings endpoint)
        accepts optional ?measurements= filter (comma-separated names)
        response: readings[].values map[string]float64
```

### PATCH /api/devices/{id} → rename device (session JWT)
```
SessionAuthenticator middleware
  └── store Claims{UserID} in context

PatchDeviceHandler
  ├── ClaimsFromContext(ctx)
  ├── decode body {name}; 400 if empty
  └── DeviceStore.PatchDevice(ctx, deviceID, userID, name)
        ├── UPDATE devices SET name=? WHERE id=? AND user_id=?
        └── 404 (ErrDeviceNotFound) if no row matched
  └── 200 {"id":"...","name":"...","created_at":"..."}
```

### POST /auth/verify → session issuance
```
VerifyHandler
  ├── AuthService.VerifyAndUpsert(provider, idToken)
  │     ├── OIDC ID token verification (go-oidc)
  │     ├── UserStore.Upsert(email, provider, sub)
  │     └── UserEventHandler.OnUserVerified → AccountStore.Upsert
  ├── AuthService.IssueSessionJWT(userID) → RS256 JWT
  └── AuthService.IssueRefreshToken(ctx, userID) → stored hash, return raw
  └── 200 {"token":"<jwt>","refresh_token":"<64-char-hex>"}
```
