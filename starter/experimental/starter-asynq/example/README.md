# starter-asynq example

A single instance plays both roles: the producer enqueues a task, the worker
(enabled in config) runs it, and the test asserts the round trip. It
self-exits (`SIGTERM`) on success; any failure exits non-zero.

## Run

```bash
docker compose up -d   # redis:7 on 127.0.0.1:6379
go run .
./check.sh             # bring up, run, tear down — the smoke test
```

Manual mode keeps the worker running:

```bash
go run . -manual
```
