# Authentication

FishHub has two separate authentication paths: **device Bearer tokens** (ESP32 → server) and **user session JWTs** (browser → server via the web frontend).

---

## Device Bearer tokens

### Token lifecycle

1. **Issue** — call `POST /tokens`. The server creates a device row and generates a cryptographically random 64-char hex token (32 bytes from `crypto/rand`).
2. **Flash** — copy the token into the ESP32 firmware's `include/config.h` as `DEVICE_TOKEN`.
3. **Authenticate** — the firmware sends `Authorization: Bearer <token>` on every `POST /readings`.

For the PoC there is no revocation. Tokens are valid until the `devices` row is deleted.

### `DeviceAuthenticator` middleware (`internal/platform/middleware.go`)

`platform.DeviceAuthenticator(devices sensors.DeviceStore)` returns a chi middleware that:

1. Reads `Authorization` header, extracts token from `"Bearer <token>"` format
2. Calls `DeviceStore.LookupByToken(ctx, token)` — a JOIN of `device_tokens + devices`
3. On success, stores `sensors.DeviceInfo{DeviceID, UserID}` in the request context via `sensors.DeviceContextKey`
4. Returns `401` if header is missing, malformed, or token unknown
5. Returns `500` on unexpected DB error

Downstream handlers retrieve it with:
```go
device, ok := sensors.DeviceFromContext(r.Context())
```

### Token storage

Tokens are stored as **plaintext** `CHAR(64)` in `device_tokens`. Acceptable for local-network PoC. Issue #6 evaluates hashing or JWT alternatives.

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
  |                  |<-- {token, refresh_token}             |
  |                  |-- set httpOnly cookies: session, refresh_token
  |<-- redirect /devices          |                          |
```

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

**Session JWT** — HS256, signed with `JWT_SECRET`. Claims: `sub` (user UUID), `iat`, `exp`. Default TTL: 24h (configurable via `JWT_TTL_HOURS`).

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

The web frontend's `apiFetch` wrapper automatically retries on `401` by calling `POST /api/auth/refresh` (a Next.js API route), which calls `POST /auth/refresh` on the server. The server rotates the refresh token and issues a new session JWT; the Next.js route updates the `session` cookie.

### Providers

Currently only `"google"` is supported. The OIDC issuer is `https://accounts.google.com`. Configured via `GOOGLE_CLIENT_ID` in the environment.
