# API Reference

Base URL: `http://localhost:8080` (development)

---

## GET /health

Health check. No authentication required.

**Response `200`**
```json
{"status": "ok"}
```

---

## POST /tokens

Creates a new device and issues a Bearer token for it. The token is hardcoded into the ESP32 firmware (`DEVICE_TOKEN` in `config.h`). Always creates the device under the seed user for the PoC.

No request body required.

**Response `201`**
```json
{
  "token":     "b0a1aba84035c6844d739100e3a93f5911f7ecaf82cbf5bbb33306a1509854a5",
  "device_id": "a1b2c3d4-...",
  "user_id":   "00000000-0000-0000-0000-000000000001"
}
```

| Field | Description |
|---|---|
| `token` | 64-char hex string. Copy into firmware `config.h` as `DEVICE_TOKEN`. |
| `device_id` | UUID of the newly created device record. |
| `user_id` | UUID of the owning user (always the seed user for the PoC). |

**Response `500`** — DB failure

---

## POST /readings

Accepts a SenML temperature reading from an authenticated device.

**Headers**
```
Authorization: Bearer <device-token>
Content-Type: application/json
```

**Request body** — SenML JSON (RFC 8428)
```json
[{
  "bn": "fishhub/device/",
  "bt": 1713000000,
  "e": [{"n": "temperature", "u": "Cel", "v": 23.4}]
}]
```

| Field | Type | Description |
|---|---|---|
| `bn` | string | Base name |
| `bt` | int64 | Base time — Unix UTC timestamp of the reading |
| `e[0].n` | string | Must be `"temperature"` |
| `e[0].u` | string | Unit — must be `"Cel"` |
| `e[0].v` | float | Temperature in Celsius |

**Response `201`** — `{}`

**Response `400`** — malformed JSON, missing `bt`, or no `temperature` entry

**Response `401`** — missing or invalid Bearer token

**Response `500`** — InfluxDB write failure

---

## POST /auth/verify

Verifies a Google OIDC ID token and issues a session JWT + refresh token. Called by the web frontend after the OAuth callback.

**Request body**
```json
{
  "provider": "google",
  "id_token": "<google-id-token>"
}
```

**Response `200`**
```json
{
  "token":         "<session-jwt>",
  "refresh_token": "<64-char-hex-refresh-token>"
}
```

**Response `400`** — missing fields

**Response `401`** — invalid ID token

**Response `422`** — unsupported provider

---

## POST /auth/refresh

Rotates a refresh token and issues a new session JWT. Old refresh token is immediately revoked (rotation).

**Request body**
```json
{
  "refresh_token": "<current-refresh-token>"
}
```

**Response `200`**
```json
{
  "token":         "<new-session-jwt>",
  "refresh_token": "<new-refresh-token>"
}
```

**Response `401`** — token not found, expired, or already revoked

---

## POST /auth/logout

Revokes the refresh token (best effort) and clears the `session` cookie.

**Request body** (optional)
```json
{
  "refresh_token": "<refresh-token>"
}
```

**Response `200`** — `{}`

---

## GET /api/me

Returns the account profile for the signed-in user.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Response `200`**
```json
{
  "id":         "<account-uuid>",
  "user_id":    "<user-uuid>",
  "email":      "user@example.com",
  "name":       "Alice",
  "created_at": "2024-04-13T12:00:00Z"
}
```

**Response `401`** — not authenticated

**Response `404`** — account not found (user exists but no account row yet)

---

## POST /api/devices/provision

Creates (or returns the existing) pending device for the authenticated user, along with a 6-character pairing code. Idempotent — repeated calls return the same pending device and code until the device is activated.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

No request body required.

**Response `201`**
```json
{
  "code":      "A1B2C3",
  "device_id": "a1b2c3d4-..."
}
```

| Field | Description |
|---|---|
| `code` | 6-char alphanumeric pairing code. Display this to the user (e.g. QR code or text) so they can enter it on the device captive portal. |
| `device_id` | UUID of the pending device. |

**Response `401`** — not authenticated

**Response `500`** — DB failure

---

## POST /devices/activate

Called by the ESP32 after the user enters the pairing code on the captive portal. No session auth required — the code itself is the credential. Marks the device active and issues a Bearer token.

No auth header required.

**Request body**
```json
{
  "code": "A1B2C3"
}
```

**Response `201`**
```json
{
  "token":      "b0a1aba84035c6844d739100e3a93f5911f7ecaf82cbf5bbb33306a1509854a5",
  "device_id":  "a1b2c3d4-...",
  "mqtt_token": "<signed-jwt>"
}
```

| Field | Description |
|---|---|
| `token` | 64-char hex Bearer token. The device stores this in NVS and uses it for all subsequent `/readings` calls. |
| `device_id` | UUID of the now-active device. |
| `mqtt_token` | RS256-signed JWT (`sub`=`device_id`, `iss`=`IDP_HOST`). The device stores this in NVS and uses it as the MQTT password when connecting to HiveMQ. Omitted if `DEVICE_JWT_PRIVATE_KEY` is not configured on the server. |

**Response `400`** — missing or empty `code`

**Response `404`** — code not found

**Response `409`** — code already used

**Response `500`** — DB or token-generation failure

---

## GET /.well-known/jwks.json

Returns the server's public key set in JWK format. Used by HiveMQ to verify device MQTT JWTs. No authentication required.

**Response `200`**
```json
{
  "keys": [
    {
      "kty": "RSA",
      "kid": "<DEVICE_JWT_KID>",
      "use": "sig",
      "alg": "RS256",
      "n":   "<base64url-encoded modulus>",
      "e":   "<base64url-encoded exponent>"
    }
  ]
}
```

Returns `{"keys":[]}` if `DEVICE_JWT_PRIVATE_KEY` is not configured.

---

## GET /api/devices

Returns devices belonging to the authenticated user. Pass `?status=active` (recommended) to exclude pending devices.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Query parameters**

| Param | Values | Default | Description |
|---|---|---|---|
| `status` | `active`, `pending`, _(empty)_ | _(empty)_ | Filter by device status. Omit to return all devices. |

**Response `200`**
```json
[
  {"id": "...", "name": "", "created_at": "2024-04-13T12:00:00Z"}
]
```

---

## PATCH /api/devices/{id}

Updates the name of a device owned by the authenticated user.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Request body**
```json
{
  "name": "Tank A"
}
```

**Response `200`**
```json
{"id": "...", "name": "Tank A", "created_at": "2024-04-13T12:00:00Z"}
```

**Response `400`** — missing or empty `name`

**Response `401`** — not authenticated

**Response `404`** — device not found or not owned by the authenticated user

**Response `500`** — DB failure

---

## GET /api/devices/{id}/readings

Returns temperature readings for a device within a time window.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Query parameters**

| Param | Format | Default | Description |
|---|---|---|---|
| `from` | RFC3339 | 24 hours ago | Start of window (inclusive) |
| `to` | RFC3339 | now | End of window (exclusive) |
| `window` | string | `"5m"` | InfluxDB aggregation window (passed through to query) |

**Response `200`**
```json
{
  "device_id": "...",
  "from": "2024-04-12T12:00:00Z",
  "to":   "2024-04-13T12:00:00Z",
  "readings": [
    {"timestamp": "2024-04-12T12:05:00Z", "temperature": 23.4}
  ]
}
```

**Response `400`** — invalid `from` or `to` format

**Response `401`** — not authenticated

**Response `404`** — device not found or not owned by the authenticated user
