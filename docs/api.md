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

## POST /readings

> **Deprecated:** devices should publish readings via MQTT to `fishhub/{device_id}/readings` instead (see fishhub-oss/fishhub-firmware#46). This endpoint remains functional as a fallback.

Accepts a SenML reading from an authenticated device.

**Headers**
```
Authorization: Bearer <device-jwt>
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
| `e[*].n` | string | Measurement name (e.g. `"temperature"`) |
| `e[*].u` | string | Unit (e.g. `"Cel"`) |
| `e[*].v` | float | Measurement value |

**Response `201`** — `{}`

**Response `400`** — malformed JSON, missing `bt`, or empty entries

**Response `401`** — missing or invalid device JWT

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

Creates (or returns the existing) pending provisioning code for the authenticated user. Idempotent — repeated calls return the same unused code until it is claimed by a device.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

No request body required.

**Response `201`**
```json
{
  "code": "A1B2C3"
}
```

| Field | Description |
|---|---|
| `code` | 6-char alphanumeric pairing code. Display this to the user (e.g. QR code or text) so they can enter it on the device captive portal. |

**Response `401`** — not authenticated

**Response `500`** — DB failure

---

## POST /devices/activate

Called by the ESP32 after the user enters the pairing code on the captive portal. No session auth required — the code itself is the credential. Creates the device row, enqueues async MQTT credential provisioning via HiveMQ, and issues a signed device JWT.

No auth header required.

**Request body**
```json
{
  "code": "A1B2C3"
}
```

**Response `202`**
```json
{
  "token":     "<signed-jwt>",
  "device_id": "a1b2c3d4-..."
}
```

| Field | Description |
|---|---|
| `token` | RS256-signed JWT. Claims: `sub` (device_id), `user_id`, `iss` (IDP_HOST), `iat`, `exp` (10 years from issuance). The device stores this in NVS and uses it as the `Authorization: Bearer` header for all subsequent requests. Empty string if `DEVICE_JWT_PRIVATE_KEY` is not configured on the server. |
| `device_id` | UUID of the newly activated device. |

MQTT credentials are provisioned asynchronously via the outbox. After receiving `202`, the device must poll `GET /devices/{id}/status` until `"status": "ready"` before attempting to connect to the MQTT broker.

**Response `400`** — missing or empty `code`

**Response `404`** — code not found

**Response `409`** — code already used

**Response `500`** — DB or token-generation failure

---

## GET /devices/{id}/status

Polls the activation status of a device. Returns MQTT credentials once HiveMQ provisioning completes. Called by the device after receiving `202` from `POST /devices/activate`.

**Headers**
```
Authorization: Bearer <device-jwt>
```

The JWT `sub` must match the `{id}` path parameter.

**Response `200` — still provisioning**
```json
{
  "status": "provisioning"
}
```

**Response `200` — ready**
```json
{
  "status":        "ready",
  "mqtt_username": "<hivemq-username>",
  "mqtt_password": "<hivemq-password>",
  "mqtt_host":     "broker.example.com",
  "mqtt_port":     8883
}
```

**Response `401`** — missing or invalid device JWT

**Response `403`** — JWT `sub` does not match `{id}`

**Response `404`** — device not found

**Response `500`** — DB failure

---

## GET /.well-known/jwks.json

Returns the server's public key set in JWK format. Used by HiveMQ to verify device MQTT JWTs. Also includes the session JWT public key. No authentication required.

**Response `200`**
```json
{
  "keys": [
    {
      "kty": "RSA",
      "kid": "<key-id>",
      "use": "sig",
      "alg": "RS256",
      "n":   "<base64url-encoded modulus>",
      "e":   "<base64url-encoded exponent>"
    }
  ]
}
```

Returns `{"keys":[]}` if no keys are configured.

---

## GET /api/devices

Returns devices belonging to the authenticated user.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

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

## DELETE /api/devices/{id}

Soft-deletes a device owned by the authenticated user. Sets `deleted_at` on the device row and revokes the device's MQTT credentials in HiveMQ.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Response `204`** — deleted

**Response `401`** — not authenticated

**Response `404`** — device not found or not owned by the authenticated user

**Response `500`** — DB failure

---

## GET /api/devices/{id}/readings

Returns sensor readings for a device within a time window.

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
| `measurements` | comma-separated strings | _(all)_ | Filter to specific measurement names (e.g. `temperature,ph`) |

**Response `200`**
```json
{
  "device_id": "...",
  "from": "2024-04-12T12:00:00Z",
  "to":   "2024-04-13T12:00:00Z",
  "readings": [
    {
      "timestamp": "2024-04-12T12:05:00Z",
      "values": {"temperature": 23.4}
    }
  ]
}
```

Each element of `readings` carries a `values` map from measurement name to float value. Multiple measurements per point are supported.

**Response `400`** — invalid `from` or `to` format

**Response `401`** — not authenticated

**Response `404`** — device not found or not owned by the authenticated user

---

## POST /api/devices/{id}/peripherals/{name}/commands

Sends a command to a named peripheral on a device via MQTT. The server publishes to the topic `fishhub/{device_id}/{peripheral_name}/commands`.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Request body**
```json
{
  "action": "set"
}
```

| Field | Values | Description |
|---|---|---|
| `action` | `"set"` \| `"schedule"` | Command action to send to the peripheral |

**Response `204`** — command published

**Response `400`** — invalid action (must be `"set"` or `"schedule"`)

**Response `401`** — not authenticated

**Response `404`** — device not found or not owned by the authenticated user

**Response `500`** — MQTT publish failure
