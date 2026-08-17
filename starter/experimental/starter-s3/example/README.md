# starter-s3 example

An object-storage round trip against MinIO: the app checks/creates the
`go-spring-example` bucket, puts an object, reads it back, stats it and
removes it again. It self-exits (`SIGTERM`) once the round trip succeeds; any
failure exits non-zero.

## Run

```bash
docker compose up -d   # MinIO :9000 (console :9001) + bucket init job
go run .
./check.sh             # bring up, run, tear down — the smoke test
```

Manual exploration mode keeps the server running:

```bash
go run . -manual
```
