# starter-xxljob example

Self-contained: the example starts a mock xxl-job admin (enough of the REST
surface to register and trigger), starts the executor, registers a handler,
triggers the job through the mock admin, and asserts the handler ran. No
docker, no real xxl-job-admin.

## Run

```bash
go run .
./check.sh   # the smoke test
```

Manual mode keeps the executor running:

```bash
go run . -manual
```
