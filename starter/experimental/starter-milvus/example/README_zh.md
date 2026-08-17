# starter-milvus 示例

一次面向 Milvus standalone 的往返：建集合（dim 8）、插入向量、加载、检索、
断言 top-1 命中。成功后自退出。

## 运行

```bash
docker compose up -d   # etcd + minio + milvus standalone
go run .
./check.sh             # 起环境、跑示例、收环境 —— 冒烟测试
```
