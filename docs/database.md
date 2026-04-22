# Database

FishHub uses **PostgreSQL** for application data (users, devices, tokens, accounts, refresh tokens) and **InfluxDB 3 Core** for time-series sensor readings.

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
├── id          UUID  PK  default gen_random_uuid()
├── user_id     UUID  FK → users.id  NOT NULL
├── name        TEXT  (nullable)
└── created_at  TIMESTAMPTZ  default now()
```

### `device_tokens`
```
device_tokens
├── id          UUID  PK  default gen_random_uuid()
├── device_id   UUID  FK → devices.id  UNIQUE NOT NULL
├── token       CHAR(64)  UNIQUE NOT NULL
└── created_at  TIMESTAMPTZ  default now()
```

One token per device (enforced by UNIQUE on `device_id`). Tokens are stored as plaintext 64-char hex strings.

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
└── created_at  TIMESTAMPTZ  NOT NULL  default now()
└── updated_at  TIMESTAMPTZ  NOT NULL  default now()
```

Created/updated automatically via `account.AccountEventHandler.OnUserVerified` on every successful OIDC login. Stores the display name from the ID token claims.

## Relationships

```
users ──< devices ──< device_tokens
users ──< refresh_tokens
users ──  accounts  (1:1 via user_id UNIQUE)
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

To add a migration, create the next numbered `.up.sql` / `.down.sql` pair in `db/migrations/`. Migrations run on the next server startup.

## Seed data

`platform.SeedUser()` runs on every startup and inserts a hardcoded device-owner user (idempotent — `ON CONFLICT DO NOTHING`):

| Field | Value |
|---|---|
| `id` | `00000000-0000-0000-0000-000000000001` |
| `email` | `admin@fishhub.local` |
| `provider` | `local` |
| `provider_sub` | `seed` |

Tokens created via `POST /tokens` are always owned by this user. No registration flow needed for the device side of the PoC.

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
| `temperature` | float (Celsius) |

The server writes one point per `POST /readings` request and queries via raw SQL in `ReadingsQueryHandler`. Connection is configured via environment variables (see [development.md](development.md)).
