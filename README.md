# 灵犀银行 · Banking Agent

面向线上银行 App 的全栈智能银行助理演示项目：用户用自然语言即可办理转账、账单分析、理财申赎、卡片管理、跨场景守护等银行业务。

> 本项目由 Kimi 平台构建，演示性质。礼品下单、清算通道等为模拟实现，未对接真实商户与核心账务系统。

## 功能

- **智能转账**：按人名/手机号/备注识别收款人（多候选自动澄清）、实时转账、定时转账、AA 收款拆分
- **账单分析**：月度/年度收支分类统计、环比、商户排行；规则引擎识别异常交易（凌晨大额、陌生对手方大额、高频异常）
- **理财**：风险测评 → 按风险等级推荐与对比产品（含适当性硬校验）→ 一键申购/赎回
- **卡片管理**：申请借记卡/信用卡、额度调整、冻结/解冻、挂失、交易限制、线上支付开关
- **跨场景守护（生日守护）**：检测联系人生日 → 生日当月自动锁定活期专款 → 生日前 N 天自动下单鲜花和蛋糕 → 生日当天送达并解锁余款（状态机：armed → locked → ordered → done）
- **Banking Agent**：LLM 工具调用（17 个工具），SSE 流式对话，大额/不可逆操作先确认，全部对话持久化

## 技术栈

- 前端：React 19 + TypeScript + Vite + Tailwind CSS + shadcn/ui + Recharts（移动端优先，桌面端居中手机栏）
- 后端：Hono + tRPC 11（端对端类型安全）
- 数据库：MySQL + Drizzle ORM
- AI：AI SDK（`streamText` + 工具调用），模型经网关接入，凭证仅服务端持有

## 本地运行

```bash
npm install
cp .env.example .env   # 填写 DATABASE_URL 等
npm run db:push          # 同步表结构
npx tsx db/seed.ts       # 写入演示数据（账户/交易/联系人/理财/卡片）
npm run dev              # http://localhost:3000
```

生产构建：`npm run build && npm start`

## 目录结构

```
api/            # Hono + tRPC 后端
  agent/        # Agent 工具集（17 个 LLM 工具）
  lib/          # 账务 helper、生日守护状态机、AI 客户端
  routers/      # 账户/转账/账单/理财/卡片/场景 tRPC 路由
  routes/       # Agent SSE 对话 REST 端点
contracts/      # 前后端共享类型
db/             # Drizzle schema 与演示种子数据
src/pages/      # 首页 / 账单 / 小犀 / 理财 / 卡包 五个页面
```
