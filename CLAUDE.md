# fishhub-server — Claude Code Instructions

## What this repo is

Go HTTP backend for FishHub. Receives temperature readings from ESP32 devices, authenticates them via Bearer tokens, and stores data for visualization. See `docs/index.md` for a full overview.

## Read the docs first

**Before making any changes, read the relevant docs:**

| File | When to read |
|------|-------------|
| `docs/index.md` | Always — start here |
| `docs/architecture.md` | Before touching package structure, handlers, or middleware |
| `docs/api.md` | Before adding or modifying endpoints |
| `docs/database.md` | Before adding migrations or changing schema |
| `docs/auth.md` | Before touching token logic or the auth middleware |
| `docs/development.md` | Before running or testing the server |

## Workflow

1. Before starting any issue, create a plan file in `../planning/` (e.g. `../planning/server-04-influxdb.md`).
2. Discuss the plan with the user before executing.
3. Implement only after the user approves.
4. Never commit directly to `main`. Always create a feature branch, commit there, and open a PR.
5. After completing an issue, move the corresponding GitHub issue to the Done column on the FishHub PoC project (`org: fishhub-oss`, project ID 1).

## Git conventions

- **Branch naming:** `feat/<slug>`, `fix/<slug>`, `chore/<slug>`, `docs/<slug>`
- **Commit style:** [Conventional Commits](https://www.conventionalcommits.org/)
  - `feat:` new feature
  - `fix:` bug fix
  - `chore:` tooling, config, deps
  - `refactor:` code change with no behavior change
  - `docs:` documentation only
- **PRs:** descriptive but concise — what changed and why. Always use `Closes #<n>` in the PR body.

## GitHub

- Org: `fishhub-oss`
- Repo: `fishhub-oss/fishhub-server`
- Project board: https://github.com/orgs/fishhub-oss/projects/1
- Issues assigned to: `renanmzmendes`

## Architecture principles

- **Dependency injection everywhere**: every dependency (DB, external clients, etc.) must be injected, never instantiated inline.
- **Depend on interfaces, not concrete types**: define the smallest interface the caller needs; pass it by interface at construction time. This applies to handlers, stores, and any service layer.

## Key conventions

- Router: `chi` v5
- Database: Postgres (application data) via `golang-migrate` for schema migrations
- Metrics: InfluxDB (time-series readings)
- Wire format for sensor readings: **SenML JSON (RFC 8428)**
- Auth: **Bearer token** (random 32-byte hex), one token per device
- New stores go in `internal/store/`, new handlers in `internal/handler/`
- Integration tests use `testutil.NewTestDB(t)` — never mock the database
