# starter-milvus example

A round trip against Milvus standalone: create a collection (dim 8), insert a
vector, load, search, and assert the top-1 hit. Self-exits on success.

## Run

```bash
docker compose up -d   # etcd + minio + milvus standalone
go run .
./check.sh             # bring up, run, tear down — the smoke test
```
