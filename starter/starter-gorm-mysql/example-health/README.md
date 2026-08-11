# starter-gorm-mysql Health Example

Demonstrates starter-gorm-mysql's **health integration**: each configured
instance gets a `health.Indicator` (pool liveness) that is folded into
starter-actuator's `/readyz` aggregate with zero per-component wiring.

## Features

- **Auto health indicator**: the starter exports one `health.Indicator` per
  instance — no application code.
- **Actuator aggregation**: `/health`, `/readyz` and `/startupz` on the
  management port (`:9370`) reflect the database pool's health.

## Manual Testing

Requires a local MySQL (`docker compose up -d`):

```bash
cd starter/starter-gorm-mysql/example-health
docker compose up -d
go run . -manual
```

Then probe the management port:

```bash
curl http://127.0.0.1:9370/readyz
curl http://127.0.0.1:9370/health
```

While the database is healthy both report `UP`.

## Smoke Test

```bash
./check.sh
```

`check.sh` brings up MySQL via docker compose, then asserts the DB pings and the
actuator probes report UP; exit code 0 means pass. Skipped gracefully when docker
is unavailable.
