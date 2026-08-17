# starter-cassandra example

A round trip against the live server (see docker-compose.yml): the app
creates the schema it needs, writes through the starter, reads back, and
asserts the result. It self-exits (`SIGTERM`) on success; any failure
exits non-zero.

## Run

```bash
docker compose up -d
go run .
./check.sh             # bring up, run, tear down — the smoke test
```

Manual exploration mode keeps the server running:

```bash
go run . -manual
```
