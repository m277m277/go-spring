# starter-gorm-sqlite example

A self-contained round trip against an in-memory SQLite database: version
query, auto-migrate, create/read/transaction-update. It self-exits
(`SIGTERM`) on success; any failure exits non-zero. No docker needed.

## Run

```bash
go run .
./check.sh   # the smoke test
```

Manual mode keeps the server running:

```bash
go run . -manual
```
