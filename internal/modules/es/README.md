# ES 模块

Elasticsearch 客户端：连接管理、集群概览、索引与文档操作。

- `module.go`：路由注册
- `client.go`：官方 Go ES client 封装
- `handler.go` / `probe.go`：索引浏览、查询、文档操作

已在 `internal/server/router.go` 注册。
