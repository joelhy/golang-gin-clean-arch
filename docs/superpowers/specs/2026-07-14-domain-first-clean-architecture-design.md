# 领域优先的 Clean Architecture 重构设计

日期：2026-07-14

## 1. 背景

当前仓库可以使用 Go 1.24 工具链编译并通过 `go vet`，但没有任何测试，且同时保留了多套互相重叠的组织方式：

- 两套用户 Controller、Repository、Use Case 和 GORM Model；
- 主程序、基础 Router、领域 Router 三套路由与组装方式；
- Wire、Module Registry 和手工组装三种依赖注入路径；
- 未接入运行路径的 CQRS、鉴权、CORS 和高级查询代码；
- 返回固定消息的订单、鉴权和高级查询占位接口；
- GORM `AutoMigrate` 与版本化 SQL migrations 并存；
- 明文密码可能进入 HTTP 响应，且当前服务缺少安全超时和优雅关闭。

这次重构不保留旧 API、旧包路径、旧数据库适配代码或旧实现的向后兼容层。目标是保留“Clean Architecture + Gin”的项目定位，同时采用符合 Go 习惯的领域优先模块化单体结构。

## 2. 目标

重构后的项目必须：

1. 完整实现用户、商品目录和订单领域，不保留伪实现或占位接口；
2. 实现短期 Access Token、可轮换 Refresh Token、会话撤销和重用检测；
3. 实现数据库驱动的 RBAC 用户、角色和权限模型；
4. 使用 MySQL、GORM 和 GORM Gen，并以版本化 SQL 作为数据库结构的唯一事实来源；
5. 在事务中完成商品锁定、服务端定价、库存扣减、订单创建和取消回补；
6. 为用户、商品和订单提供过滤、排序、分页，并提供管理员统计查询；
7. 使用 Wire 完成编译期依赖组装，但不为了依赖注入制造无业务价值的接口；
8. 以 Go 1.25 为最低版本；
9. 提供单元测试、Gin HTTP 契约测试和基于临时 MySQL 容器的集成测试；
10. 具备严格配置校验、结构化日志、安全中间件、健康检查和优雅关闭能力。

## 3. 非目标

本次不实现：

- 旧 `/api/v1` 路由行为、旧 Go 包或旧占位响应的兼容层；
- 支付、退款、物流供应商、优惠券或多仓库存；
- 分布式消息队列、事件总线或微服务拆分；
- 多租户、社交登录或完整的找回密码流程；
- Redis 等额外基础设施；
- 以目录层级模拟企业规模的预留抽象。

## 4. 架构

### 4.1 包结构

采用领域优先的模块化单体：

```text
cmd/
  server/          HTTP 服务入口、信号处理和 Wire 注入器
  admin/           migration 与初始管理员命令及 Wire 注入器
user/              用户、凭据、会话、角色和权限规则
product/           商品目录、价格和库存规则
order/             订单、订单项、状态机和事务用例
reporting/         跨领域管理员统计的只读查询模型
web/               Gin 路由、Handler、DTO、中间件和响应映射
mysqlstore/         GORM 模型、GORM Gen 查询和端口实现
security/           Argon2id、JWT 和安全随机 Token
config/             强类型环境配置加载与校验
migrations/         可追踪、可回滚的版本化 SQL
```

不再保留当前 `domain/application/adapters/modules/infrastructure` 的深层目录，也不保留 Module Registry。Clean Architecture 由依赖方向保证，而不是由目录层级数量保证。

### 4.2 依赖规则

- `user`、`product`、`order` 和 `reporting` 不导入 Gin、GORM、MySQL 驱动或 JWT 库。
- 领域包定义业务服务实际消费的最小端口；`mysqlstore` 和 `security` 提供具体实现。
- `web` 只负责 HTTP 协议转换、输入校验、身份上下文、权限调用和错误映射。
- `cmd/server` 与 `cmd/admin` 是依赖组装边界，不包含业务规则。
- Wire 仅连接具体构造函数和必要接口绑定。生成的 `wire_gen.go` 提交到仓库，不手工修改。
- 不创建 `utils`、`helpers` 或通用 Repository 等无法表达所有权的包。

### 4.3 跨领域事务

下单需要同时读取商品、扣减库存并保存订单。为避免领域包之间横向导入，`order` 定义它所消费的事务端口：事务执行器、可售商品视图、库存预留和订单保存。`mysqlstore` 在同一 `*gorm.DB` 事务中实现这些端口。

业务规则仍由 `order` 服务控制；数据库适配器只提供锁、持久化和原子提交语义。这样既保证库存与订单一致，又不会把 GORM 事务对象泄漏到领域层。

## 5. 领域设计

### 5.1 用户、会话与 RBAC

用户包含：

- 数字 ID；
- 规范化后的唯一邮箱；
- 显示名；
- 密码哈希；
- `active` 或 `disabled` 状态；
- 乐观并发版本号；
- 创建与更新时间。

公开注册创建普通用户并授予 `customer` 角色。初始管理员只能通过显式管理命令创建。系统必须拒绝停用、删除角色或撤销系统最后一个可用管理员的管理员权限。

RBAC 使用 `users`、`roles`、`permissions`、`user_roles` 和 `role_permissions` 表。权限采用稳定字符串，例如：

- `users:read`、`users:write`、`users:roles`；
- `products:write`、`products:stock`；
- `orders:read_all`、`orders:manage`；
- `stats:read`。

Access Token 只携带用户 ID、会话 ID、唯一 Token ID、签发时间、签发者和受众，不携带角色或权限快照。每个受保护请求都检查用户状态、会话状态和当前权限，因此撤权和会话注销无需等待 Access Token 到期。

Refresh Token 是一次性高熵随机值。数据库仅保存其 SHA-256 摘要、轮换版本、到期时间和撤销信息。刷新操作在事务中消费旧 Token 并生成新 Token；检测到已使用 Token 再次出现时，撤销整个会话。

### 5.2 商品与库存

商品包含：

- 唯一 SKU；
- 名称和描述；
- 以最小货币单位表示的整数价格；
- ISO 4217 币种；
- 非负库存数量；
- 上架状态；
- 乐观并发版本号；
- 创建与更新时间。

管理员可以创建和修改商品、上下架商品并通过库存调整操作增减库存。库存调整必须保存变化量、变化后库存、原因、操作者和时间，不能通过普通商品更新静默覆盖库存。

普通用户只能查询上架商品。下架商品不能加入新订单，但其历史订单快照保持可见。

### 5.3 订单

订单包含用户、订单号、状态、币种、总金额、订单项、版本号及审计时间。订单项保存下单时的商品 ID、SKU、名称、单价、数量和小计快照。

订单状态转换为：

```text
pending -> confirmed -> shipped -> delivered
   |            |
   +----------> cancelled
```

业务约束：

- 客户只能为自己创建、查看和取消订单；
- 客户只能取消 `pending` 订单；管理员可以取消 `pending` 或 `confirmed` 订单；
- 管理员负责确认、发货和送达操作；
- 任何非法状态转换都返回稳定的领域错误；
- 取消订单必须在同一事务中回补库存并写入库存流水；
- 已送达或已取消订单不可再次变更状态。

创建订单要求客户端提供幂等键。幂等键按“用户 + 操作 + 键”唯一。相同键和相同请求返回首次创建的订单；相同键但请求内容不同返回冲突，不能再次扣减库存。

## 6. 数据与事务

### 6.1 主要数据表

- `users`
- `roles`
- `permissions`
- `user_roles`
- `role_permissions`
- `sessions`、`refresh_tokens`
- `products`
- `stock_adjustments`
- `orders`
- `order_items`
- `idempotency_keys`

### 6.2 数据约束

- 规范化邮箱、SKU、角色名和权限标识必须唯一；
- 金额使用 `BIGINT` 保存最小货币单位，不使用浮点数；
- 库存、数量和金额必须通过数据库约束保证非负；
- 外键、唯一键和高频过滤字段由 migration 显式建立索引；
- 用户、商品和订单通过状态和审计字段保留历史，不使用物理删除破坏引用；
- 订单项保存不可变价格与商品信息快照。

### 6.3 下单事务

下单在单个 MySQL 事务中执行：

1. 校验或占用幂等键；
2. 将商品 ID 排序后使用 `SELECT ... FOR UPDATE` 依次加锁；
3. 校验商品上架状态、币种、数量和可用库存；
4. 从锁定记录读取服务端价格并生成订单项快照；
5. 扣减库存并写入库存调整流水；
6. 计算订单总额并写入订单与订单项；
7. 记录幂等请求摘要和结果订单 ID；
8. 提交事务。

固定锁顺序用于降低并发死锁概率。适配器只对明确可重试的 MySQL 死锁或锁等待错误执行有限次数重试，并遵守请求 Context 的取消与截止时间。

### 6.4 迁移与代码生成

- 版本化 SQL 是 Schema 的唯一事实来源；服务启动不执行 `AutoMigrate`。
- `cmd/admin migrate up|down` 显式执行迁移。
- migrations 初始化系统角色和权限，但不写入管理员密码。
- GORM Gen 基于专用持久化模型生成查询代码。
- 生成代码提交到仓库并由测试验证可重复生成；业务代码不得手工修改生成文件。
- Go 1.25 `tool` 指令固定 Wire、GORM Gen 和 migration 工具版本。

## 7. HTTP API

API 使用新的 `/api/v1` 契约，不兼容旧接口。

### 7.1 路由

```text
POST   /api/v1/auth/register
POST   /api/v1/auth/login
POST   /api/v1/auth/refresh
POST   /api/v1/auth/logout

GET    /api/v1/users/me
PATCH  /api/v1/users/me
GET    /api/v1/admin/users
GET    /api/v1/admin/users/:id
PATCH  /api/v1/admin/users/:id/status
PUT    /api/v1/admin/users/:id/roles

GET    /api/v1/products
GET    /api/v1/products/:id
POST   /api/v1/admin/products
PATCH  /api/v1/admin/products/:id
POST   /api/v1/admin/products/:id/stock-adjustments

POST   /api/v1/orders
GET    /api/v1/orders
GET    /api/v1/orders/:id
POST   /api/v1/orders/:id/cancel
GET    /api/v1/admin/orders
POST   /api/v1/admin/orders/:id/confirm
POST   /api/v1/admin/orders/:id/ship
POST   /api/v1/admin/orders/:id/deliver
POST   /api/v1/admin/orders/:id/cancel

GET    /api/v1/admin/stats
GET    /health/live
GET    /health/ready
```

### 7.2 请求流

```text
Gin
 -> Request ID / Recovery / Security Headers / CORS / Access Log
 -> JWT 与会话校验
 -> RBAC 权限校验
 -> Handler DTO 严格校验
 -> 领域服务
 -> 消费方端口
 -> GORM Gen / MySQL
```

Handler 限制请求体大小并拒绝未知 JSON 字段。Controller/Handler 不包含状态转换、库存或权限业务规则。

### 7.3 统一响应

成功响应只要求 `code` 和可选 `data`：

```json
{
  "code": 0,
  "data": {
    "id": 42,
    "email": "user@example.com"
  }
}
```

失败响应要求非零 `code` 和非空 `message`，业务错误上下文可放入 `data`：

```json
{
  "code": 10001,
  "message": "request validation failed",
  "data": {
    "fields": [
      {
        "field": "email",
        "reason": "invalid_email",
        "message": "email must be a valid address"
      }
    ]
  }
}
```

响应约束：

- `code == 0` 表示成功；非零表示失败；
- 成功时省略 `message`；
- 失败时 `message` 必须存在且非空；
- `data` 承载资源、分页和业务错误上下文；没有数据时省略；
- 不提供 `success`、`meta`、`details` 或 `request_id` JSON 字段；
- 请求 ID 只进入 `X-Request-ID` 响应头和结构化日志。

错误码范围：

- `1xxxx`：请求解析和字段校验；
- `2xxxx`：认证、会话和权限；
- `3xxxx`：用户与 RBAC；
- `4xxxx`：商品与库存；
- `5xxxx`：订单和幂等；
- `9xxxx`：内部错误。

HTTP 状态码仍准确表达协议语义。内部错误、SQL 文本、堆栈和敏感安全信息不得进入响应。

### 7.4 分页与查询

列表数据把分页作为 `data` 的组成部分：

```json
{
  "code": 0,
  "data": {
    "items": [],
    "pagination": {
      "limit": 20,
      "offset": 0,
      "total": 137,
      "has_more": true
    }
  }
}
```

所有排序字段和方向必须使用白名单。最大页大小固定，排序总是追加 ID 作为稳定次序。

- 用户支持邮箱、名称、状态和角色过滤；
- 商品支持 SKU、名称、上下架状态和库存状态过滤；
- 订单支持用户、状态、创建时间和金额区间过滤；
- 管理员统计支持明确起止时间并返回用户、库存和订单汇总。

## 8. 安全设计

### 8.1 密码与 Token

- 密码使用 Argon2id、独立随机盐和带参数的标准编码；
- Argon2id 参数集中管理，使旧哈希可以在登录成功后渐进升级；
- Access Token 使用 HMAC-SHA-256，默认有效期 15 分钟；
- Refresh Token 使用 `crypto/rand` 生成至少 256 位随机值，默认有效期 30 天；
- 服务启动必须拒绝过短、缺失或示例 JWT 密钥；
- JWT 验证明确定义签名算法、签发者、受众和时间声明，不接受任意算法。

### 8.2 HTTP 安全

- 登录、注册和刷新按 IP 与账号维度限流；
- 登录失败使用一致消息，避免确认账号是否存在；
- CORS 使用配置白名单，不组合通配来源和 credentials；
- 可信代理显式配置，默认不信任任意转发头；
- 加入适用于 JSON API 的安全响应头；
- 请求体大小、读取时间和写入时间均受限；
- Recovery 中间件记录 panic，但不向客户端返回堆栈。

Authorization、密码、密码哈希、Refresh Token、数据库凭据和完整 Cookie 不得进入日志。

## 9. 配置、日志与生命周期

`config.Load()` 返回强类型配置与错误，并校验：

- HTTP 地址和各类超时；
- MySQL DSN 与连接池范围；
- JWT 密钥、签发者、受众和 Token TTL；
- CORS 来源、可信代理和请求体上限；
- Argon2id 参数与登录限流配置；
- 管理命令所需参数。

`.env` 只作为本地开发便利，生产环境不依赖它。仓库不提供可直接用于生产的默认密码或 JWT 密钥。

服务使用注入的 `*slog.Logger`。开发模式可以使用文本 Handler，生产模式使用 JSON Handler。错误沿调用链返回，只在 HTTP 或命令入口等系统边界记录，避免同一错误被重复记录。

`http.Server` 必须配置 Header、Read、Write 和 Idle 超时。服务通过信号 Context 停止接收新请求，使用独立截止时间排空在途请求，最后关闭底层 `sql.DB`。所有后台清理任务必须监听 Context 或关闭通道，确保有清晰退出路径。

`/health/live` 只验证进程存活；`/health/ready` 使用短超时检查数据库。MySQL 连接池配置最大连接数、空闲连接数、连接最大生命周期和空闲生命周期。

## 10. 管理命令

`cmd/admin` 提供：

- `migrate up`：升级至最新数据库版本；
- `migrate down`：按明确步数回滚，危险操作要求显式确认参数；
- `bootstrap-admin`：通过参数或安全交互输入创建初始管理员。

管理员引导复用 `user` 服务及密码、角色规则，不能直接写表绕过约束。命令必须返回错误而不是在业务层调用 `os.Exit`；只有 `main` 决定进程退出码。

## 11. 测试策略

### 11.1 单元测试

使用标准库 `testing`、表驱动测试和手写 fake，覆盖：

- 用户规范化、状态和最后管理员规则；
- 密码验证、登录、会话撤销、刷新轮换与重用检测；
- 角色权限判断；
- 商品校验和库存调整；
- 订单金额、快照、状态转换、幂等和错误分支。

时间、随机数和 ID 生成器作为依赖注入。并发与超时测试使用 Go 1.25 的确定性测试能力或显式同步，不使用 `time.Sleep` 等待 goroutine。

### 11.2 HTTP 契约测试

使用 `httptest` 和 fake 服务验证：

- 路由和权限矩阵；
- 严格 JSON 解析与请求体限制；
- HTTP 状态码和稳定业务错误码；
- 成功响应 `code == 0` 且没有 `message`；
- 失败响应 `code != 0` 且包含非空 `message`；
- 分页位于 `data.pagination`；
- 敏感字段不进入响应或日志。

### 11.3 MySQL 集成测试

使用 Testcontainers 启动临时 MySQL，覆盖：

- migration 升级与回滚；
- GORM Gen 查询和数据库约束；
- Refresh Token 原子轮换与重用撤销；
- 幂等下单；
- 并发库存扣减不超卖；
- 取消订单回补库存；
- 锁顺序和可重试事务错误。

普通 `go test ./...` 不要求 Docker；MySQL 测试使用 `integration` 构建标签显式执行。

### 11.4 交付门禁

交付前执行：

```text
gofmt
go test ./...
go test -race ./...
go test -tags=integration ./...
go vet ./...
go build ./cmd/server ./cmd/admin
docker build
```

生成代码不计入手写业务覆盖率指标。

## 12. 文档与注释

README 和相关文档必须与真实代码、命令和 API 一致，并覆盖：

- 架构与依赖规则；
- 本地启动、配置和 Docker；
- migration 与 GORM Gen；
- 管理员引导；
- API、错误码和权限表；
- 单元测试与集成测试。

删除无法验证的“无限扩展”“100+ 开发者”等宣传性描述和所有伪实现说明。

重构时保留仍适用的现有注释并随逻辑迁移；语义发生变化时同步更新。新增注释聚焦于事务、锁顺序、幂等、令牌轮换、权限边界、重试和错误降级背后的原因，不机械复述代码。

## 13. 验收标准

重构完成需同时满足：

1. 仓库只保留一套可运行的组装、路由和领域实现；
2. 用户、商品、库存、订单、会话和 RBAC 路径不存在占位 Handler；
3. 数据库结构完全由版本化 SQL 管理，启动过程不调用 `AutoMigrate`；
4. GORM Gen 查询可以重复生成，生成结果与仓库一致；
5. 并发下单不会产生负库存或重复订单；
6. Refresh Token 轮换、撤销和重用检测通过集成测试；
7. API 响应符合统一 `code/message/data` 契约；
8. 业务包不依赖 Gin、GORM、MySQL 或 JWT 具体实现；
9. 服务具备安全超时、健康检查、结构化日志和优雅关闭；
10. 所有质量门禁命令通过，README 可指导新使用者从空数据库启动系统。
