# Database

FishHub uses **PostgreSQL** for application data (users, devices, tokens). InfluxDB will be added for time-series metrics (issue #4).

## Schema

```
users
├── id          UUID  PK  default gen_random_uuid()
├── email       TEXT  UNIQUE NOT NULL
└── created_at  TIMESTAMPTZ  default now()

devices
├── id          UUID  PK  default gen_random_uuid()
├── user_id     UUID  FK → users.id  NOT NULL
├── name        TEXT  (nullable)
└── created_at  TIMESTAMPTZ  default now()

device_tokens
├── id          UUID  PK  default gen_random_uuid()
├── device_id   UUID  FK → devices.id  UNIQUE NOT NULL
├── token       CHAR(64)  UNIQUE NOT NULL
└── created_at  TIMESTAMPTZ  default now()
```

**Relationships:**
- One user → many devices
- One device → one token (enforced by UNIQUE on `device_id`)

## Migrations

Migrations live in `db/migrations/` and follow the `golang-migrate` naming convention:

```
NNN_<description>.up.sql
NNN_<description>.down.sql
```

They run automatically on server startup via `db.Migrate()`. The current migrations are:

| # | Description |
|---|---|
| 001 | Create `users` table |
| 002 | Create `devices` table |
| 003 | Create `device_tokens` table |

To add a new migration, create the next numbered pair of `.up.sql` / `.down.sql` files in `db/migrations/`.

## Seed data

On every startup, `db.SeedUser()` inserts a hardcoded admin user (idempotent — uses `ON CONFLICT DO NOTHING`):

| Field | Value |
|---|---|
| `id` | `00000000-0000-0000-0000-000000000001` |
| `email` | `admin@fishhub.local` |

All tokens created via `POST /tokens` are owned by this user. This is intentional for the PoC — no registration flow is needed.

## Connection

The server reads `DATABASE_URL` from the environment:

```
DATABASE_URL=postgres://fishhub:fishhub@localhost:5432/fishhub?sslmode=disable
```

The default in the Makefile matches the credentials in `docker-compose.yml`.
