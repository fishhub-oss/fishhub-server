# Database

FishHub uses **PostgreSQL** for application data (users, devices, accounts, refresh tokens, outbox events) and **InfluxDB 3 Core** for time-series sensor readings.

## Schema

### `users`
```
users
├── id           UUID  PK  default gen_random_uuid()
├── email        TEXT  UNIQUE NOT NULL
├── provider     TEXT  NOT NULL  default 'local'
├── provider_sub TEXT  NOT NULL  default ''
└── created_at   TIMESTAMPTZ  default now()

UNIQUE (provider, provider_sub)
```

Stores one row per identity. The `provider`/`provider_sub` pair identifies the OAuth account (e.g. `provider='google'`, `provider_sub='<google-sub-claim>'`). The seed user has `provider='local'`.

### `device_models`
```
device_models
├── id         UUID        PK  default gen_random_uuid()
├── slug       VARCHAR(64) UNIQUE NOT NULL
├── name       VARCHAR(128) NOT NULL
└── created_at TIMESTAMPTZ NOT NULL  default now()
```

Hardware model catalogue. Currently only `fishhub-v1` is seeded. Each model defines a fixed set of ports (see `device_model_ports`).

### `device_model_ports`
```
device_model_ports
├── id         UUID        PK  default gen_random_uuid()
├── model_id   UUID        FK → device_models.id  NOT NULL
├── kind       VARCHAR(32) NOT NULL  -- e.g. 'relay', 'ds18b20', 'analog'
├── label      VARCHAR(32) NOT NULL  -- e.g. 'RELAY 1', 'TEMP', 'ANALOG 2'
├── pin        INT         NOT NULL  -- GPIO pin number
└── created_at TIMESTAMPTZ NOT NULL  default now()

UNIQUE (model_id, pin)
UNIQUE (model_id, kind, label)
```

One row per physical port on a hardware model. The `fishhub-v1` seed data:

| Label | Kind | Pin |
|-------|------|-----|
| TEMP | ds18b20 | 4 |
| RELAY 1 | relay | 16 |
| RELAY 2 | relay | 17 |
| RELAY 3 | relay | 26 |
| RELAY 4 | relay | 27 |
| ANALOG 1 | analog | 32 |
| ANALOG 2 | analog | 33 |
| ANALOG 3 | analog | 34 |
| ANALOG 4 | analog | 35 |

### `devices`
```
devices
├── id             UUID  PK  default gen_random_uuid()
├── user_id        UUID  FK → users.id  NOT NULL
├── model_id       UUID  FK → device_models.id  NOT NULL
├── name           TEXT  (nullable)
├── mqtt_username  TEXT  (nullable)
├── mqtt_password  TEXT  (nullable)
├── deleted_at     TIMESTAMPTZ  (nullable)
└── created_at     TIMESTAMPTZ  default now()
```

A device row is created when the ESP32 claims a pairing code via `POST /devices/activate`. `model_id` is set to `fishhub-v1` at claim time. `mqtt_username` and `mqtt_password` are populated asynchronously by the outbox runner after HiveMQ provisioning completes. `deleted_at` is set on soft-delete; soft-deleted devices are excluded from all queries.

### `provisioning_codes`
```
provisioning_codes
├── id         UUID  PK  default gen_random_uuid()
├── code       CHAR(6)  UNIQUE NOT NULL
├── user_id    UUID  FK → users.id  NOT NULL
├── device_id  UUID  (nullable)
├── used_at    TIMESTAMPTZ  (nullable)
└── created_at TIMESTAMPTZ  default now()
```

Created when a web user calls `POST /api/devices/provision`. `device_id` is null until the code is claimed — the device row does not exist yet at provisioning time. `used_at` is set atomically when the ESP32 calls `POST /devices/activate`; the `WHERE used_at IS NULL` guard makes the claim race-safe. Once claimed the code cannot be reused.

### `refresh_tokens`
```
refresh_tokens
├── id          UUID  PK  default gen_random_uuid()
├── user_id     UUID  FK → users.id ON DELETE CASCADE  NOT NULL
├── token_hash  CHAR(64)  UNIQUE NOT NULL
├── expires_at  TIMESTAMPTZ  NOT NULL
├── revoked_at  TIMESTAMPTZ  (nullable)
└── created_at  TIMESTAMPTZ  default now()

INDEX refresh_tokens_user_id_idx ON (user_id)
```

Stores the SHA-256 hash of the raw refresh token (never the raw token). Rotated on every use — old token is revoked, new token is issued.

### `accounts`
```
accounts
├── id          UUID  PK  default gen_random_uuid()
├── user_id     UUID  UNIQUE NOT NULL
├── email       TEXT  NOT NULL
├── name        TEXT  NOT NULL  default ''
├── created_at  TIMESTAMPTZ  NOT NULL  default now()
└── updated_at  TIMESTAMPTZ  NOT NULL  default now()
```

Created/updated automatically via `account.AccountEventHandler.OnUserVerified` on every successful OIDC login. Stores the display name from the ID token claims.

### `outbox_events`
```
outbox_events
├── id                    UUID  PK  default gen_random_uuid()
├── event_type            TEXT  NOT NULL
├── payload               JSONB  NOT NULL
├── status                TEXT  NOT NULL  default 'pending'  -- 'pending' | 'processing' | 'completed' | 'dead'
├── created_at            TIMESTAMPTZ  NOT NULL  default now()
├── claimed_at            TIMESTAMPTZ  (nullable)
├── claim_timeout_seconds INT  NOT NULL  default 300
├── attempts              INT  NOT NULL  default 0
└── last_error            TEXT  (nullable)

INDEX outbox_events_claimable ON (event_type, created_at)
  WHERE status = 'pending' OR status = 'processing'
```

Stores pending side-effects that must be processed asynchronously. Currently used for HiveMQ device provisioning: after `POST /devices/activate`, an event of type `hivemq.provision` is inserted here. The outbox runner polls this table, calls the HiveMQ REST API, and writes `mqtt_username` / `mqtt_password` back to the `devices` row on success. Events that fail `maxAttempts` times (default: 5) are moved to status `'dead'`.

### `triggers`
```
triggers
├── id         UUID  PK  default gen_random_uuid()
├── device_id  UUID  FK → devices.id  NOT NULL
├── name       TEXT  NOT NULL
├── enabled    BOOL  NOT NULL  default true
├── condition  JSONB NOT NULL
├── cooldown_s INT   NOT NULL  default 60
├── deleted_at TIMESTAMPTZ  (nullable)
└── created_at TIMESTAMPTZ  NOT NULL  default now()

INDEX triggers_device_id_idx ON (device_id) WHERE deleted_at IS NULL
```

Each trigger belongs to one device. `condition` is an opaque JSONB expression tree — the server stores and forwards it without interpreting it; evaluation happens on the firmware. Soft-deleted triggers (`deleted_at IS NOT NULL`) are excluded from all list/get queries.

### `actions`
```
actions
├── id         UUID  PK  default gen_random_uuid()
├── type       TEXT  NOT NULL
├── config     JSONB NOT NULL
└── created_at TIMESTAMPTZ  NOT NULL  default now()
```

Stores the what-to-do for a trigger. `type` is always `"peripheral_action"` in Phase 1. `config` is a JSONB object with shape:
```json
{
  "peripheral_id": "<peripheral-uuid>",
  "command": "set" | "set_mode",
  "value": <number> | "automatic" | "manual"
}
```

### `action_triggers`
```
action_triggers
├── trigger_id  UUID  FK → triggers.id  ON DELETE CASCADE  NOT NULL
└── action_id   UUID  FK → actions.id   ON DELETE CASCADE  NOT NULL

PRIMARY KEY (trigger_id, action_id)
INDEX action_triggers_trigger_id_idx ON (trigger_id)
```

Join table linking triggers to their actions. Phase 1 always has exactly one action per trigger, but the schema is open for future multi-action support.

### `peripherals`
```
peripherals
├── id           UUID  PK  default gen_random_uuid()
├── device_id    UUID  FK → devices.id  NOT NULL
├── name         TEXT  NOT NULL
├── kind         TEXT  NOT NULL  -- e.g. 'relay', 'ds18b20', 'analog'
├── pin          INT   NOT NULL  -- GPIO pin number (copied from port at creation)
├── port_id      UUID  FK → device_model_ports.id  (nullable for legacy rows)
├── category     TEXT  NOT NULL  -- 'sensor' | 'actuator'
├── control_mode TEXT  (nullable)  -- 'automatic' | 'manual'; non-null for actuators only
├── schedule     JSONB NOT NULL  default '[]'
├── deleted_at   TIMESTAMPTZ  (nullable)
├── created_at   TIMESTAMPTZ  NOT NULL  default now()
└── updated_at   TIMESTAMPTZ  NOT NULL  default now()

UNIQUE INDEX peripherals_device_name_active_idx ON (device_id, name) WHERE deleted_at IS NULL
UNIQUE INDEX peripherals_device_pin_active_idx  ON (device_id, pin)  WHERE deleted_at IS NULL
UNIQUE INDEX peripherals_port_id_active_idx     ON (port_id)         WHERE deleted_at IS NULL AND port_id IS NOT NULL
```

One row per physical peripheral attached to a device. `port_id` links to the model port that determines the GPIO pin; `pin` is copied from the port at insertion so it remains correct even if the port catalogue changes. Legacy rows created before `port_id` was added have `port_id = NULL`. Soft-deleted peripherals (`deleted_at IS NOT NULL`) are excluded from all list/get queries but their port slot is released for reuse.

## Relationships

```
device_models ──< device_model_ports
              └──< devices
users ──< devices
      └──< provisioning_codes
      └──< refresh_tokens
      └──  accounts  (1:1 via user_id UNIQUE)
devices ──< peripherals ──> device_model_ports (via port_id, nullable)
        └──< triggers
triggers ──< action_triggers ──> actions
outbox_events  (standalone — no FK; device/trigger referenced via payload)
```

## Migrations

Migrations live in `db/migrations/` and use the `golang-migrate` naming convention:

```
NNN_<description>.up.sql
NNN_<description>.down.sql
```

They run automatically on server startup via `platform.Migrate()`. Current migrations:

| # | Description |
|---|---|
| 001 | Create `users` table |
| 002 | Create `devices` table |
| 003 | Create `device_tokens` table |
| 004 | Add `provider` + `provider_sub` columns to `users` |
| 005 | Create `refresh_tokens` table |
| 006 | Create `accounts` table |
| 007 | Add `status` column to `devices` |
| 008 | Create `provisioning_codes` table |
| 009 | Drop `device_tokens` table |
| 010 | Add `mqtt_username`, `mqtt_password` columns to `devices` |
| 011 | Add `deleted_at` column to `devices` |
| 012 | Make `provisioning_codes.device_id` nullable; add `user_id` column |
| 013 | Drop `status` column from `devices` |
| 014 | Create `outbox_events` table |
| 015 | Create `peripherals` table |
| 016 | Add unique index on peripheral pin per device |
| 017 | Add `category` and `control_mode` columns to `peripherals` |
| 018 | Add `timezone` column to `accounts` |
| 019 | Create `triggers` table |
| 020 | Create `actions` table |
| 021 | Create `action_triggers` join table |
| 022 | Migrate existing trigger rows to `actions` + `action_triggers`; drop `target_peripheral_id` and `action` columns from `triggers` |
| 023–025 | (internal) |
| 026 | Create `device_models` and `device_model_ports` tables; seed `fishhub-v1` |
| 027 | Add `model_id` (NOT NULL) to `devices`; backfill existing rows to `fishhub-v1` |
| 028 | Add `port_id` (nullable) to `peripherals`; add unique index `peripherals_port_id_active_idx` |

To add a migration, create the next numbered `.up.sql` / `.down.sql` pair in `db/migrations/`. Migrations run on the next server startup.

## Seed data

`platform.SeedUser()` runs on every startup and inserts a hardcoded device-owner user (idempotent — `ON CONFLICT DO NOTHING`):

| Field | Value |
|---|---|
| `id` | `00000000-0000-0000-0000-000000000001` |
| `email` | `admin@fishhub.local` |
| `provider` | `local` |
| `provider_sub` | `seed` |

## Connection

The server reads `DATABASE_URL` from the environment:

```
DATABASE_URL=postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable
```

The default in the Makefile matches the credentials in `docker-compose.yml`.

## InfluxDB

Time-series readings are written to InfluxDB 3 Core. The `sensors` measurement uses these tags and fields:

| Tag | Value |
|---|---|
| `device_id` | UUID of the device |
| `user_id` | UUID of the owning user |

| Field | Value |
|---|---|
| _(measurement name)_ | float — one field per named measurement in the SenML payload (e.g. `temperature`) |

The server writes one point per `POST /readings` request and queries via raw SQL in `ReadingsQueryHandler`. Connection is configured via environment variables (see [development.md](development.md)).
