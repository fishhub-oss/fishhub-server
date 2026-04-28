# Authentication

FishHub has two separate authentication paths: **device JWTs** (ESP32 → server) and **user session JWTs** (browser → server via the web frontend).

---

## Device JWTs

### Token lifecycle

1. Web user calls `POST /api/devices/provision` (session JWT required) — receives a 6-char pairing `code`.
2. User enters the code on the device captive portal (or transmits it via QR code).
3. ESP32 calls `POST /devices/activate` with `{ "code": "..." }` (no auth). The server claims the code atomically, creates the device row, enqueues async MQTT provisioning, and returns `{ token, device_id }` with `202 Accepted`.
4. The firmware stores the JWT in NVS and sends `Authorization: Bearer <token>` on every subsequent request (`POST /readings`, `GET /devices/{id}/status`).

### JWT structure

Device JWTs are RS256-signed tokens produced by `devicejwt.Signer` (backed by `jwtutil.Signer`).

**Claims:**

| Claim | Value |
|---|---|
| `sub` | Device UUID |
| `user_id` | Owner user UUID |
| `iss` | `IDP_HOST` env var |
| `iat` | Unix timestamp of issuance |
| `exp` | `iat` + 10 years |

The corresponding public key is served at `GET /.well-known/jwks.json` (alongside the session JWT public key). HiveMQ uses this endpoint to verify device MQTT JWTs.

When `DEVICE_JWT_PRIVATE_KEY` is not configured, `devicejwt.NewNoOp()` is used — `Sign()` returns `""` and `PublicKey()` returns `nil`.

### `DeviceAuthenticator` middleware (`internal/platform/middleware.go`)

`platform.DeviceAuthenticator(signer devicejwt.Signer)` returns a chi middleware that:

1. Reads `Authorization` header, extracts token from `"Bearer <token>"` format
2. Validates the token as an RS256 JWT using `signer.PublicKey()` — no database lookup
3. Extracts `sub` (device_id) and `user_id` claims from the validated JWT
4. On success, stores `sensors.DeviceInfo{DeviceID, UserID}` in the request context via `sensors.DeviceContextKey`
5. Returns `401` if the header is missing, the token is malformed, the signature is invalid, or required claims are absent

Downstream handlers retrieve it with:
```go
device, ok := sensors.DeviceFromContext(r.Context())
```

There is no revocation mechanism — a device JWT is valid until its `exp` claim (10 years from issuance). To invalidate a device, soft-delete it; the `DeviceStore` excludes soft-deleted devices from all lookups.

---

## MQTT credentials

MQTT credentials (`mqtt_username`, `mqtt_password`) are distinct from the device JWT. They are used to authenticate the device directly to the HiveMQ broker over TLS.

### Lifecycle

1. `POST /devices/activate` inserts a `hivemq.provision` event into `outbox_events` (within the same DB transaction as the device row creation).
2. The outbox runner picks up the event and calls `hivemq.Client.ProvisionDevice(username, password)`:
   - `POST /mqtt/credentials` on the HiveMQ Cloud REST API
   - `PUT /user/{username}/roles/{roleID}/attach` to grant the device role
3. On success, `mqtt_username` and `mqtt_password` are written to the `devices` row.
4. `GET /devices/{id}/status` returns `"status": "ready"` with the credentials once the device row has non-null values and no pending/processing outbox event exists for the device.
5. `DELETE /api/devices/{id}` calls `hivemq.Client.DeleteDevice(mqttUsername)` (best effort) to revoke the credentials in HiveMQ.

---

## User session auth (OIDC + JWT)

### Flow overview

```
Browser                    Next.js (fishhub-web)       fishhub-server
  |                               |                          |
  |-- click "Sign in with Google" |                          |
  |-- GET /api/auth/login/google  |                          |
  |<-- redirect to Google OAuth   |                          |
  |                               |                          |
  |-- Google redirect callback    |                          |
  |-- GET /api/auth/callback/google?code=...                 |
  |                  |-- exchange code → id_token            |
  |                  |-- POST /auth/verify {provider, id_token}
  |                  |                         |-- OIDC verify
  |                  |                         |-- DB upsert user
  |                  |                         |-- create/update account
  |                  |                         |-- issue session JWT + refresh token
  |                  |<-- 200 {token, refresh_token}         |
  |                  |-- store JWT in localStorage; set httpOnly cookie: session
  |<-- redirect /devices          |                          |
```

`POST /auth/verify` returns `{ "token": "<session-jwt>", "refresh_token": "<64-char-hex>" }` as JSON (status 200). The Next.js callback handler stores the JWT in `localStorage` for client-side API calls and also sets a `session` httpOnly cookie so SSR pages can read it.

### `AuthService` (`internal/auth/service.go`)

Interface:
```go
type AuthService interface {
    VerifyAndUpsert(ctx, provider, idToken string) (User, error)
    IssueSessionJWT(userID string) (string, error)
    ValidateSessionJWT(token string) (string, error)
    IssueRefreshToken(ctx, userID string) (string, error)
    RotateRefreshToken(ctx, rawToken string) (newRawToken, sessionJWT string, err error)
    RevokeRefreshToken(ctx, rawToken string) error
}
```

Implemented by `oidcService`. Configured via `auth.NewOIDCService(ctx, OIDCConfig{...})` in `main.go`.

**`VerifyAndUpsert`** — verifies the ID token with the OIDC provider (go-oidc library), upserts the user row, then calls `UserEventHandler.OnUserVerified` (implemented by `account.AccountEventHandler`, which upserts the account row with name/email from ID token claims).

**Session JWT** — RS256, signed with a dedicated RSA private key (`SESSION_JWT_PRIVATE_KEY`). Claims: `sub` (user UUID), `iat`, `exp`. Default TTL: 24h (configurable via `JWT_TTL_HOURS`). The corresponding public key is included in `GET /.well-known/jwks.json`.

**Refresh tokens** — 64-char raw hex token; stored as SHA-256 hash in `refresh_tokens`. TTL: 30 days. Rotation: every `RotateRefreshToken` call revokes the old token and issues a new one.

### `SessionAuthenticator` middleware (`internal/platform/middleware.go`)

`platform.SessionAuthenticator(svc auth.AuthService)` returns a chi middleware that:

1. Reads `Authorization: Bearer <token>` header, or falls back to the `session` cookie
2. Calls `AuthService.ValidateSessionJWT(token)` → `userID`
3. On success, stores `auth.Claims{UserID}` in context via `auth.ContextWithClaims`
4. Returns `401` if token is absent or invalid

Downstream handlers retrieve it with:
```go
claims, ok := auth.ClaimsFromContext(r.Context())
```

### Refresh token rotation (web frontend)

The web frontend's `apiFetch` wrapper automatically retries on `401` by calling `POST /api/auth/refresh` (a Next.js API route), which calls `POST /auth/refresh` on the server. The server rotates the refresh token and issues a new `{ token, refresh_token }` pair; the Next.js route updates `localStorage` and the `session` cookie with the new JWT.

### Providers

Currently only `"google"` is supported. The OIDC issuer is `https://accounts.google.com`. Configured via `GOOGLE_CLIENT_ID` in the environment.
