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

Creates a new device and issues an authentication token for it. The token is what gets hardcoded into the ESP32 firmware (`DEVICE_TOKEN` in `config.h`).

No request body required. The token is always created for the seed user (`admin@fishhub.local`).

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
| `token` | 64-char hex string. Copy this into `config.h` as `DEVICE_TOKEN`. |
| `device_id` | UUID of the newly created device record. |
| `user_id` | UUID of the owning user (always the seed user for the PoC). |

**Response `500`** — internal error (DB failure)

---

## POST /readings

Accepts a SenML temperature reading from an authenticated device.

**Headers**
```
Authorization: Bearer <token>
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
| `bn` | string | Base name (device namespace) |
| `bt` | int64 | Base time — Unix UTC timestamp of the reading |
| `e[0].n` | string | Must be `"temperature"` |
| `e[0].u` | string | Unit — must be `"Cel"` |
| `e[0].v` | float | Temperature in Celsius |

**Response `201`**
```json
{}
```

**Response `400`** — malformed JSON, missing `bt`, or no `temperature` entry

**Response `401`** — missing or invalid Bearer token
