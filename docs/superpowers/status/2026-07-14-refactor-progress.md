# 领域优先重构状态

更新时间：2026-07-15

## 当前结论

领域优先 Clean Architecture 重构已经进入收尾验证阶段。旧 `internal/` 架构、旧 `cmd/main.go` 入口和旧用户面文档已经由新的领域包、`cmd/server`、`cmd/admin`、`web`、`mysqlstore`、`docs/architecture.md`、`docs/api.md` 和 `docs/error-codes.md` 取代。

## 当前运行入口

```text
cmd/server  HTTP API, Wire runtime graph, listener and graceful shutdown
cmd/admin   migration and bootstrap-admin commands
```

## 当前验证重点

Task 14 的最终验证需要确认：

- `go generate ./...` 可重复生成 GORM Gen 和 Wire 文件；
- `go test ./...`、`go test -race ./...`、`go test -tags=integration ./...`、`go vet ./...` 通过；
- `go build ./cmd/server ./cmd/admin` 通过；
- `docker build -t clean-arch-gin:test .` 通过，或记录具体 Docker daemon 错误；
- `git diff --check` 无空白问题。

## 参考文档

- 当前架构：`docs/architecture.md`
- 当前 API：`docs/api.md`
- 错误码：`docs/error-codes.md`
