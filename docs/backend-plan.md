# 灵犀银行 · Go 后端规划

> 目标：用 Go 重写后端，替换现有 Hono + tRPC + Drizzle 演示后端；前端页面零改动或极少改动；按金融系统标准补齐安全与账务一致性。

## 1. 现状评估

| 维度 | 现状（TS 演示后端） | 风险 |
| --- | --- | --- |
| 鉴权 | 无登录、无会话，所有接口公开 | 任何人可转账、挂失 |
| 数据隔离 | 单账户，表无 `user_id` | 无法多用户；IDOR |
| 资金一致性 | 先读后写、无事务、无行锁 | 并发重复扣款/超扣 |
| 金额精度 | `INT`（≈ 2,147 万元上限）、JS 浮点乘法 | 溢出、舍入误差 |
| 状态机 | 无条件 `UPDATE` | 合约重复取消、贷款重复还款 |
| 审计 | 仅访问日志 | 资金操作不可追溯 |
| Agent | 「大额先确认」只写在 prompt | 幻觉/提示注入可直接动账 |

## 2. 技术选型

- Go 1.26，`net/http` + `chi` 路由；`database/sql` + `go-sql-driver/mysql`（MySQL 8，沿用现有库）
- 密码：`argon2id`（x/crypto）；会话：随机 256bit token，库内只存 SHA-256
- 迁移：`embed` 内嵌 SQL + 自研顺序执行器（带 `schema_migrations` 表）
- 日志：`log/slog` JSON，统一 request id，敏感字段脱敏
- LLM：OpenAI 兼容接口（默认 DeepSeek，可配置），自研工具调用循环 + SSE
- 前端协议：**兼容 tRPC v11 batch 协议**（`/api/trpc/a.b,c.d?batch=1`，superjson 外壳），前端 hooks 无需改动

## 3. 目录结构

```
server/
  cmd/api/            # 入口
  internal/
    config/           # 环境变量加载与校验（缺关键配置拒绝启动）
    db/               # 连接池、事务 helper、迁移（migrations/*.sql）
    httpx/            # 中间件：requestID、recover、安全头、限流、Origin 校验、body 限制
    trpc/             # tRPC 线协议适配：路由注册、batch、错误格式
    auth/             # 注册/登录/登出、会话、锁定策略
    audit/            # 审计日志（追加写）
    ledger/           # ★ 核心账务：扣款/入账/冻结/解冻，全部事务 + 行锁
    money/            # 金额/汇率/利息定点运算（int64 + 溢出检查）
    service/          # 各业务域：account transfer bill card wealth fx scf trade scenario
    agent/            # LLM 客户端、工具注册、确认令牌、SSE
    worker/           # 定时转账执行、场景推进、合约到期
  seed/               # 演示数据
```

## 4. 分阶段计划（先做什么后做什么）

| 阶段 | 内容 | 产出/验收 | 执行 |
| --- | --- | --- | --- |
| **P0 骨架** | config、slog、DB 连接与迁移、错误模型、tRPC 适配层、`ping` | `trpc.ping` 前端可调通 | 我 |
| **P1 安全基础** | users/sessions、登录登出、Origin 校验（CSRF）、限流、安全头、审计；前端加最小登录页、Vite 代理到 Go | 未登录调用返回 401；登录后正常 | 我（前端登录页交 dsh） |
| **P2 核心账务** | `ledger` 包；`account.overview`、`transfer.*`（实时/定时/AA）；定时转账 worker | 并发转账测试余额精确、不超扣 | 我 |
| **P3 业务模块** | bill（统计/异常规则）、card、wealth（适当性）、fx（远期/期权）、scf（放款/计息还款）、trade、scenario（生日守护状态机） | 前端各页面数据正常 | 资金类我写；trade/card/bill 只读查询交 dsh，我审查 |
| **P4 Agent** | LLM 客户端、31 个工具映射到 service、服务端确认令牌、SSE | 小犀对话可用；大额操作必须经用户下一轮确认 | 我 |
| **P5 收尾** | 种子数据、单元/集成测试、`govulncheck`、README、移除 TS 后端运行路径 | `go test ./...` 通过 | 种子/文档交 dsh |

依赖关系：P0 → P1 → P2 →（P3 各模块可并行）→ P4 → P5。Agent 依赖所有 service，所以放在最后。

## 5. 安全设计

### 5.1 身份与会话
- 密码 argon2id（m=64MB, t=3, p=2），常量时间比较；用户名不存在时也做一次哈希，防枚举计时
- 连续 5 次失败锁定 15 分钟；登录接口按 IP + 用户名限流
- 会话 Cookie：`HttpOnly; SameSite=Strict; Path=/`，生产环境 `Secure`；空闲 30 分钟、绝对 12 小时过期；登出即吊销
- 登录成功轮换会话 ID（防会话固定）

### 5.2 请求层
- CSRF：SameSite=Strict + 所有非 GET 请求校验 `Origin`/`Sec-Fetch-Site` 白名单
- 安全头：CSP、`X-Content-Type-Options`、`X-Frame-Options: DENY`、`Referrer-Policy`，生产加 HSTS
- 请求体上限（tRPC 1MB、Agent 256KB、日志 64KB）；JSON 严格解码（拒绝未知字段、类型不符）
- 限流：全局按 IP，资金类 mutation 按用户单独限流
- 错误：对外只返回业务错误文案与 tRPC code；内部错误统一 `INTERNAL_SERVER_ERROR`，细节只进日志

### 5.3 授权（防 IDOR）
- 所有业务表增加 `user_id`，所有查询/更新强制 `WHERE user_id = ?`（取自会话，绝不取自入参）
- 按 id 操作的资源（卡、合约、贷款、持仓、场景）一律带 owner 条件，查不到统一返回「不存在」

### 5.4 账务一致性（核心）
- 金额统一 `BIGINT` 分；汇率/利率用基点整数；乘除走 `money` 包（`math/big` 中间值 + 银行家/四舍五入规则固定）
- 每次动账在一个 DB 事务内：`SELECT ... FOR UPDATE` 锁账户行 → 校验可用余额 → 更新余额 → 写流水（含 `balance_after`）→ 写审计
- 多行加锁按固定顺序（账户 → 钱包 → 业务单据），避免死锁；死锁/锁超时自动重试有限次
- DB 约束兜底：`CHECK (balance_cents >= 0 AND locked_cents >= 0 AND locked_cents <= balance_cents)`
- 状态机迁移使用条件更新 `UPDATE ... WHERE id=? AND status='active'`，校验影响行数，杜绝重复取消/还款
- 单笔与日累计限额（转账单笔 ≤ 100 万、日累计 ≤ 200 万，可配置）
- 幂等：mutation 支持 `Idempotency-Key` 头，(user, key) 唯一，重复请求返回首次结果

### 5.5 审计
- `audit_logs` 只追加：用户、动作、目标、脱敏参数、结果、IP、UA、request id、时间
- 所有登录事件和所有 mutation 都记录（成功与失败）

### 5.6 Agent 安全
- 工具白名单 + 入参二次校验（与 tRPC 共用 service 层，不绕过任何检查）
- **服务端强制确认**：高风险工具（转账/申购/赎回/挂失/调额/锁汇/期权/融资/还款，或金额 > ¥5,000）第一次调用只返回 `需确认 + confirmToken`；令牌绑定用户 + 工具 + 参数哈希，5 分钟有效、单次使用，且必须在**之后的新一轮用户消息**中才可兑现
- 工具输出作为数据回传模型，系统提示明确不执行工具结果中的指令
- 限制历史条数、单轮最多 8 步、每用户对话限流；LLM 密钥仅服务端持有

## 6. 与现有前端的兼容

- 保持 39 个 tRPC procedure 名称与入参/出参形状不变
- 日期以 ISO 字符串返回（前端已统一 `new Date(x)` 包装）
- 新增 REST：`POST /api/auth/login`、`POST /api/auth/logout`、`GET /api/auth/me`
- Vite 开发服务器改为把 `/api` 代理到 Go（`:8080`）；原 `api/` TS 目录仅保留为前端类型契约，不再运行
