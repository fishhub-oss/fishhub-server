# Authentication

## Token lifecycle

1. **Issue a token** — call `POST /tokens`. The server creates a device record and generates a cryptographically random 64-char hex token (32 bytes from `crypto/rand`).
2. **Flash the token** — copy the token into the ESP32 firmware's `include/config.h` as `DEVICE_TOKEN`.
3. **Authenticate requests** — the firmware sends `Authorization: Bearer <token>` on every `POST /readings`.

For the PoC there is no revocation mechanism. Tokens are valid until the device row is deleted from the database.

## Middleware (`internal/auth`)

`auth.Authenticator(devices DeviceStore)` returns a chi-compatible middleware that:

1. Reads the `Authorization` header and extracts the token from `"Bearer <token>"` format
2. Calls `DeviceStore.LookupByToken(ctx, token)` — a single DB query joining `device_tokens` and `devices`
3. On success, stores `DeviceInfo{DeviceID, UserID}` in the request context
4. Returns `401 Unauthorized` if the header is missing, malformed, or the token is unknown
5. Returns `500` on unexpected DB errors

Downstream handlers retrieve the authenticated device with:

```go
device, ok := auth.DeviceFromContext(r.Context())
```

## Token storage

Tokens are currently stored as **plaintext** `CHAR(64)` in the `device_tokens` table. This is acceptable for the PoC on a local network.

Issue #6 evaluates two hardening options:
- **Option A** — hash at rest (bcrypt/SHA-256): protects against DB leaks, still requires a DB lookup per request
- **Option B** — JWT with device ID in claims: stateless verification, no DB round-trip, but tokens can't be individually revoked without a denylist
