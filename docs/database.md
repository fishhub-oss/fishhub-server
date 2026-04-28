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

### `devices`
```
devices
├── id             UUID  PK  default gen_random_uuid()
├── user_id        UUID  FK → users.id  NOT NULL
├── name           TEXT  (nullable)
├── mqtt_username  TEXT  (nullable)
├── mqtt_password  TEXT  (nullable)
├── deleted_at     TIMESTAMPTZ  (nullable)
└── created_at     TIMESTAMPTZ  default now()
```

A device row is created when the ESP32 claims a pairing code via `POST /devices/activate`. `mqtt_username` and `mqtt_password` are populated asynchronously by the outbox runner after HiveMQ provisioning completes. `deleted_at` is set on soft-delete; soft-deleted devices are excluded from all queries.

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

## Relationships

```
users ──< devices
      └──< provisioning_codes
      └──< refresh_tokens
      └──  accounts  (1:1 via user_id UNIQUE)
outbox_events  (standalone — no FK; device referenced via payload)
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
