# API Reference

Base URL: `http://localhost:8080` (development)

---

## Error responses

All error responses return JSON with a consistent structure:

```json
{
  "code": "snake_case_error_code",
  "message": "Human-readable description"
}
```

`code` is a stable machine-readable identifier — clients should switch on this, never on `message`. `message` is for humans (logs, dev tools) and may change without notice.

| HTTP status | `code` | Situation |
|---|---|---|
| 400 | `invalid_request` | Malformed JSON, missing required fields, or invalid query params |
| 401 | `unauthorized` | Missing or invalid auth |
| 403 | `forbidden` | Authenticated but not allowed (e.g. JWT sub mismatch) |
| 404 | `device_not_found` | Device does not exist or is not owned by the caller |
| 404 | `peripheral_not_found` | Peripheral does not exist |
| 404 | `account_not_found` | Account does not exist |
| 404 | `provisioning_code_not_found` | Provisioning code does not exist |
| 409 | `peripheral_name_conflict` | Active peripheral with the same name already exists |
| 409 | `peripheral_pin_conflict` | Active peripheral with the same pin already exists |
| 409 | `provisioning_code_conflict` | Provisioning code has already been used |
| 404 | `trigger_not_found` | Trigger does not exist or belongs to a different device |
| 422 | `invalid_request` | Unsupported OIDC provider |
| 500 | `internal_error` | Unexpected server error |

---

## GET /health

Health check. No authentication required.

**Response `200`**
```json
{"status": "ok"}
```

---

## POST /auth/verify

Verifies an OAuth credential and issues a session JWT + refresh token. Called by the web frontend after the OAuth callback.

The request body differs by provider:

**Google (OIDC)**
```json
{
  "provider": "google",
  "id_token": "<google-id-token>"
}
```

**GitHub (OAuth 2.0)**
```json
{
  "provider": "github",
  "code": "<github-oauth-authorization-code>"
}
```

For `google`, supply `id_token` (the OIDC ID token from the Google OAuth callback). For `github`, supply `code` (the OAuth authorization code from the GitHub callback); the server exchanges it for an access token and fetches the user profile from the GitHub API.

**Response `200`**
```json
{
  "token":         "<session-jwt>",
  "refresh_token": "<64-char-hex-refresh-token>"
}
```

**Response `400`** — missing or invalid fields

**Response `401`** — invalid credential (bad ID token or failed GitHub code exchange)

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
  "timezone":   "UTC",
  "created_at": "2024-04-13T12:00:00Z"
}
```

**Response `401`** — not authenticated

**Response `404`** — account not found (user exists but no account row yet)

---

## PATCH /api/me

Updates the timezone for the signed-in user. On success, enqueues a retained MQTT config push to `fishhub/<device_id>/config` for every device owned by the user.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Request body**
```json
{
  "timezone": "America/Sao_Paulo"
}
```

| Field | Description |
|---|---|
| `timezone` | IANA timezone name (e.g. `"UTC"`, `"America/New_York"`, `"Europe/Lisbon"`). Must be a valid name recognised by Go's `time.LoadLocation`. |

**Response `200`** — updated account profile (same shape as `GET /api/me`)

**Response `400`** — missing or empty `timezone`, or unrecognised IANA zone name

**Response `401`** — not authenticated

**Response `404`** — account not found

**Response `500`** — DB or outbox failure

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

---

## POST /api/devices/{id}/triggers

Creates a new trigger for the device.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Request body**
```json
{
  "name": "Heater on cold",
  "condition": {
    "op": "lt",
    "left":  { "op": "value",   "measurement": "ds18b20-4/temperature" },
    "right": { "op": "literal", "value": 19.0 }
  },
  "actions": [
    {
      "type": "peripheral_action",
      "config": {
        "peripheral_id": "<peripheral-uuid>",
        "command": "set",
        "value": 1.0
      }
    }
  ],
  "cooldown_s": 60
}
```

| Field | Required | Description |
|---|---|---|
| `name` | yes | Human-readable label |
| `condition` | yes | JSON expression tree (see firmware `docs/peripherals.md` for the full operator reference) |
| `actions` | yes | Array of action objects (Phase 1: exactly one entry, type `peripheral_action`) |
| `actions[].type` | yes | Must be `"peripheral_action"` |
| `actions[].config.peripheral_id` | yes | UUID of the peripheral to actuate — must be an `actuator` peripheral owned by the same device |
| `actions[].config.command` | yes | `"set"` or `"set_mode"` |
| `actions[].config.value` | yes | Value to pass to the peripheral (number for `set`; `"automatic"` or `"manual"` for `set_mode`) |
| `cooldown_s` | no | Minimum seconds between firings (default: `60`, must be `>= 0`) |

**Validation:**
- `actions` must contain exactly one entry.
- `actions[0].type` must be `"peripheral_action"`.
- `actions[0].config.peripheral_id` must be non-empty and refer to a peripheral with `category = "actuator"` belonging to the same device.
- `actions[0].config.command` must be `"set"` or `"set_mode"`.

**Response `201`**
```json
{
  "id":        "<uuid>",
  "name":      "Heater on cold",
  "enabled":   true,
  "condition": { ... },
  "actions": [
    {
      "id":     "<action-uuid>",
      "type":   "peripheral_action",
      "config": {
        "peripheral_id": "<peripheral-uuid>",
        "command": "set",
        "value": 1.0
      }
    }
  ],
  "cooldown_s":  60,
  "created_at":  "2024-04-13T12:00:00Z"
}
```

**Response `400`** — missing required field, invalid `actions` (wrong count, unknown type, missing `peripheral_id`, invalid `command`), `cooldown_s < 0`, or `peripheral_id` not an actuator owned by the device

**Response `401`** — not authenticated

**Response `404`** — device not found or not owned by the authenticated user

**Response `500`** — DB or outbox failure

---

## GET /api/devices/{id}/triggers

Returns all non-deleted triggers for the device.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Response `200`**
```json
[
  {
    "id":        "<uuid>",
    "name":      "Heater on cold",
    "enabled":   true,
    "condition": { ... },
    "actions": [
      {
        "id":     "<action-uuid>",
        "type":   "peripheral_action",
        "config": {
          "peripheral_id": "<peripheral-uuid>",
          "command": "set",
          "value": 1.0
        }
      }
    ],
    "cooldown_s":  60,
    "created_at":  "2024-04-13T12:00:00Z"
  }
]
```

Returns `[]` if the device has no triggers.

**Response `401`** — not authenticated

**Response `500`** — DB failure

---

## GET /api/devices/{id}/triggers/{tid}

Returns a single trigger.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Response `200`** — trigger object (same shape as create response)

**Response `401`** — not authenticated

**Response `404`** — `trigger_not_found` — trigger does not exist or belongs to a different device

**Response `500`** — DB failure

---

## PATCH /api/devices/{id}/triggers/{tid}

Partially updates a trigger. All fields are optional — only supplied fields are changed. Omitting a field (or sending `null`) leaves it unchanged.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Request body** (all fields optional)
```json
{
  "name":       "Heater on cold (updated)",
  "enabled":    false,
  "condition":  { ... },
  "actions": [
    {
      "type": "peripheral_action",
      "config": {
        "peripheral_id": "<peripheral-uuid>",
        "command": "set",
        "value": 1.0
      }
    }
  ],
  "cooldown_s": 120
}
```

**Validation (same as create, applied only to provided fields):**
- `name` must not be an empty string if provided.
- `actions`, if provided, must pass the same validation as create (exactly one entry, valid type, non-empty `peripheral_id`, valid `command`).
- `cooldown_s` must be `>= 0` if provided.

On success, publishes an updated `upsert` MQTT message to `fishhub/{device_id}/triggers/{trigger_id}` via the outbox.

**Response `200`** — updated trigger object (same shape as create response)

**Response `400`** — validation failure

**Response `401`** — not authenticated

**Response `404`** — `trigger_not_found`

**Response `500`** — DB or outbox failure

---

## DELETE /api/devices/{id}/triggers/{tid}

Soft-deletes a trigger (sets `deleted_at`). Publishes a `{"op":"delete","id":"..."}` MQTT message to `fishhub/{device_id}/triggers/{trigger_id}` via the outbox, then publishes an empty payload to clear the retained message on the broker.

**Headers** (one of):
```
Authorization: Bearer <session-jwt>
Cookie: session=<session-jwt>
```

**Response `204`** — deleted

**Response `401`** — not authenticated

**Response `404`** — `trigger_not_found`

**Response `500`** — DB or outbox failure
