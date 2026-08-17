# starter-webhook example

A self-contained notification round trip: the app starts a local HTTP
receiver standing in for a chat-platform webhook, sends a notification
through the starter, and asserts the JSON payload that arrives. It
self-exits (`SIGTERM`) on success; any failure exits non-zero. No docker
needed.

## Run

```bash
go run .
./check.sh   # the smoke test
```

Manual mode keeps the receiver running so a real Notifier can be driven by
hand:

```bash
go run . -manual   # receiver on http://127.0.0.1:18080/hook
```
