# 领域优先重构交接状态

更新时间：2026-07-14

## 当前结论

重构尚未完成。当前已完成基础配置、数据库结构、用户/RBAC 领域、安全组件以及认证/会话 MySQL 适配器；商品、订单、HTTP API、运行时组装、旧架构清理和最终端到端验证尚未实施。

继续开发时使用分支：

```text
refactor/domain-first-clean-architecture-impl
```

当前实现提交：

```text
058fe2c fix(auth): 修正令牌校验与用户错误映射
```

## 分支关系

两个重构分支的共同祖先是：

```text
7a0978b chore(git): 忽略隔离工作树目录
```

### `refactor/domain-first-clean-architecture`

这是最初的设计与实施计划分支，位于主工作区。它在共同祖先之后只有一个独有提交：

```text
7784378 docs(plan): 改用标准库环境配置
```

该分支没有后续实现代码，也没有设置远端跟踪分支。

### `refactor/domain-first-clean-architecture-impl`

这是通过 Git worktree 创建的隔离实现分支。它从共同祖先开始承载所有实际重构代码，已推送到：

```text
origin/refactor/domain-first-clean-architecture-impl
```

设计分支的 `7784378` 已通过等价提交 `7b30b73` 带入实现分支，并在 `2fba654` 中进一步更新为使用 `urfave/cli`。因此后续只需要继续和合并 `-impl` 分支，不应再把两个分支依次合并，否则可能产生重复文档提交或冲突。

## 任务拆分与状态

| 任务 | 内容 | 实施状态 | 审查状态 |
|---|---|---|---|
| Task 1 | Go 1.25、依赖与严格配置基线 | 已实施 | 规格与质量审查通过 |
| Task 2 | 版本化 Schema、migration runner、GORM Gen 模型 | 已实施 | 规格与质量审查通过；Docker 实跑受阻 |
| Task 3 | 用户、RBAC 与账户领域 | 已实施 | 规格与质量审查通过 |
| Task 4 | Argon2id、JWT、Refresh Token 与随机 ID | 已实施 | 规格与质量审查通过 |
| Task 4A | 使用标准库替换 Viper 配置，切换 urfave/cli 依赖 | 已实施 | 规格与质量审查通过 |
| Task 5 | 会话服务、UserStore、RBAC 与 SessionStore | 已实施 | 规格审查通过；最终质量审查未完成 |
| Task 6 | 商品与库存领域 | 未实施 | 未开始 |
| Task 7 | 商品与库存 MySQL 适配器 | 未实施 | 未开始 |
| Task 8 | 订单聚合、状态机、查询与结算服务 | 未实施 | 未开始 |
| Task 9 | 订单事务 MySQL 适配器 | 未实施 | 未开始 |
| Task 10 | 统一 HTTP 响应、严格解码与安全中间件 | 未实施 | 未开始 |
| Task 11 | 认证、用户与商品 HTTP Handler | 未实施 | 未开始 |
| Task 12 | 订单、统计、完整路由与 API 契约 | 未实施 | 未开始 |
| Task 13 | urfave/cli 管理命令、Wire 与优雅服务生命周期 | 未实施 | 未开始 |
| Task 14 | 移除旧架构、更新部署文档并端到端验证 | 未实施 | 未开始 |

## 已实施内容

### Task 1 与 Task 4A：配置和依赖

- 项目最低版本升级到 Go 1.25。
- 配置使用标准库 `os.LookupEnv`、`strconv` 和 `time` 显式解析 `APP_*` 环境变量。
- Viper、Cobra 和 mapstructure 已从当前源码及编译依赖中移除。
- 管理 CLI 后续使用 `github.com/urfave/cli/v3`，但 CLI 本身尚未实现。
- 配置具备严格空值、溢出、时长、CORS、数据库池、JWT 和 Argon2 参数校验。

### Task 2：数据库基础

- 建立 12 张业务表的可逆 MySQL migration。
- 初始化角色、权限和管理员权限映射，不写入用户凭据。
- 应用连接与 migration 多语句连接已经隔离。
- GORM Gen 查询可重复生成。
- 已编写 migration、约束和生成查询的 Testcontainers 集成测试。

### Task 3：用户与 RBAC

- 实现账户注册、管理员引导、资料更新、筛选和分页规则。
- 实现角色与权限常量、权限精确匹配和最后管理员保护。
- Store 契约要求使用共享串行化 guard 防止并发 write-skew。
- 领域返回值执行深拷贝，避免适配器缓存或切片别名污染授权状态。

### Task 4：安全组件

- Argon2id 密码哈希、严格 PHC 解析和资源上限。
- 仅允许 HS256 的 JWT 签发与验证。
- 256 位 Refresh Token 与 SHA-256 持久化摘要。
- 128 位会话、Token 和 JWT ID 生成器。

### Task 5：认证、会话和 MySQL 适配器

- 登录、密码透明升级、Access Token 身份验证和实时权限检查。
- Refresh Token 原子轮换、旧 Token 重用检测和会话族撤销。
- UserStore 支持角色/权限加载、筛选、分页和乐观版本更新。
- 最后管理员状态/角色变更使用相同的 `admin` guard 锁顺序。
- SessionStore 在提交重用撤销后才返回 `ErrRefreshReuse`，避免安全状态被事务回滚。
- Task 5 已通过规格审查，但用户暂停工作时，独立代码质量审查尚未完成。

## 未完成和阻塞项

### Docker/Testcontainers

当前环境没有可用 Docker provider：

```text
rootless Docker not found, failed to create Docker provider
```

因此 integration-tag 测试已经编译，但没有在当前环境完成真实容器 GREEN。恢复工作后需要在 Docker daemon 可用时执行：

```bash
go test -count=1 -tags=integration ./...
```

### 旧架构仍然存在

`internal/` 下的旧 Controller、Use Case、Repository、Module Registry 和占位接口仍然存在。它们要等新 HTTP/CLI/Server 路径可运行后，在 Task 14 统一删除。当前仓库能编译不代表新架构已经接管程序入口。

### 尚无完整可运行 API

商品、订单、Gin Handler、完整路由、管理 CLI、Wire 组装与新 Server 入口均未实现。因此当前分支是阶段性代码，不是可发布版本。

## 恢复开发顺序

1. 检出并更新实现分支：

   ```bash
   git switch refactor/domain-first-clean-architecture-impl
   git pull --ff-only
   ```

2. 运行非容器基线：

   ```bash
   go test -count=1 ./...
   go test -race -count=1 ./user ./security ./mysqlstore
   go vet ./...
   ```

3. 完成 Task 5 的独立代码质量审查，修复 Critical/Important 问题后复审。
4. Docker 可用时运行全部 integration-tag 测试。
5. 从 Task 6 商品与库存领域继续执行实施计划。
6. 每个任务仍按“实现代理 → 规格审查 → 质量审查”顺序完成后再进入下一项。

## 参考文档

- 设计规格：`docs/superpowers/specs/2026-07-14-domain-first-clean-architecture-design.md`
- 实施计划：`docs/superpowers/plans/2026-07-14-domain-first-clean-architecture.md`
