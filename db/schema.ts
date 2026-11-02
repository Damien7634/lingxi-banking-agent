import {
  mysqlTable,
  mysqlEnum,
  serial,
  bigint,
  varchar,
  text,
  int,
  boolean,
  timestamp,
} from "drizzle-orm/mysql-core";

// ============ 账户 ============
export const accounts = mysqlTable("accounts", {
  id: serial("id").primaryKey(),
  holderName: varchar("holder_name", { length: 64 }).notNull(),
  // 活期总余额（含被锁定部分），单位：分
  balanceCents: int("balance_cents").notNull().default(0),
  // 其中被场景任务锁定的金额，单位：分
  lockedCents: int("locked_cents").notNull().default(0),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
});

// ============ 交易流水 ============
export const transactions = mysqlTable("transactions", {
  id: serial("id").primaryKey(),
  direction: mysqlEnum("direction", ["in", "out"]).notNull(),
  amountCents: int("amount_cents").notNull(),
  category: varchar("category", { length: 32 }).notNull().default("其他"),
  counterparty: varchar("counterparty", { length: 128 }).notNull(),
  memo: varchar("memo", { length: 255 }),
  channel: varchar("channel", { length: 32 }).notNull().default("手机银行"),
  status: mysqlEnum("status", ["success", "pending", "failed"])
    .notNull()
    .default("success"),
  isAnomaly: boolean("is_anomaly").notNull().default(false),
  anomalyReason: varchar("anomaly_reason", { length: 255 }),
  occurredAt: timestamp("occurred_at").notNull().defaultNow(),
});

// ============ 收款人 ============
export const contacts = mysqlTable("contacts", {
  id: serial("id").primaryKey(),
  name: varchar("name", { length: 64 }).notNull(),
  phone: varchar("phone", { length: 20 }),
  bankName: varchar("bank_name", { length: 64 }),
  cardTail: varchar("card_tail", { length: 8 }),
  relation: varchar("relation", { length: 32 }),
  birthday: varchar("birthday", { length: 10 }), // MM-DD
  note: varchar("note", { length: 255 }),
});

// ============ 定时转账 ============
export const scheduledTransfers = mysqlTable("scheduled_transfers", {
  id: serial("id").primaryKey(),
  contactId: bigint("contact_id", { mode: "number", unsigned: true }).notNull(),
  amountCents: int("amount_cents").notNull(),
  memo: varchar("memo", { length: 255 }),
  executeAt: timestamp("execute_at").notNull(),
  status: mysqlEnum("status", ["pending", "executed", "cancelled"])
    .notNull()
    .default("pending"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ AA 收款 ============
export const aaSplits = mysqlTable("aa_splits", {
  id: serial("id").primaryKey(),
  title: varchar("title", { length: 128 }).notNull(),
  totalCents: int("total_cents").notNull(),
  // JSON: [{ name, phone, amountCents, paid }]
  participants: text("participants").notNull(),
  status: mysqlEnum("status", ["collecting", "done", "cancelled"])
    .notNull()
    .default("collecting"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ 银行卡 ============
export const cards = mysqlTable("cards", {
  id: serial("id").primaryKey(),
  cardType: mysqlEnum("card_type", ["debit", "credit"]).notNull(),
  bankName: varchar("bank_name", { length: 64 }).notNull(),
  last4: varchar("last4", { length: 4 }).notNull(),
  creditLimitCents: int("credit_limit_cents"),
  usedCents: int("used_cents").notNull().default(0),
  dailyLimitCents: int("daily_limit_cents").notNull().default(5000000),
  status: mysqlEnum("status", ["normal", "frozen", "lost", "restricted"])
    .notNull()
    .default("normal"),
  onlinePayEnabled: boolean("online_pay_enabled").notNull().default(true),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ 理财产品 ============
export const wealthProducts = mysqlTable("wealth_products", {
  id: serial("id").primaryKey(),
  name: varchar("name", { length: 128 }).notNull(),
  type: mysqlEnum("type", ["cash", "bond", "mixed", "index", "gold"]).notNull(),
  riskLevel: mysqlEnum("risk_level", ["R1", "R2", "R3", "R4", "R5"]).notNull(),
  // 业绩比较基准，单位:基点(万分之一), 例 235 = 2.35%
  annualRateBps: int("annual_rate_bps").notNull(),
  minAmountCents: int("min_amount_cents").notNull().default(100),
  termDays: int("term_days").notNull().default(0), // 0 = 灵活申赎
  tags: varchar("tags", { length: 128 }),
  description: varchar("description", { length: 255 }),
});

// ============ 理财持仓 ============
export const holdings = mysqlTable("holdings", {
  id: serial("id").primaryKey(),
  productId: bigint("product_id", { mode: "number", unsigned: true }).notNull(),
  amountCents: int("amount_cents").notNull(),
  profitCents: int("profit_cents").notNull().default(0),
  status: mysqlEnum("status", ["holding", "redeemed"])
    .notNull()
    .default("holding"),
  purchasedAt: timestamp("purchased_at").notNull().defaultNow(),
  redeemedAt: timestamp("redeemed_at"),
});

// ============ 风险测评 ============
export const riskProfiles = mysqlTable("risk_profiles", {
  id: serial("id").primaryKey(),
  score: int("score").notNull(),
  level: varchar("level", { length: 16 }).notNull(), // 保守/稳健/平衡/进取/激进
  answers: text("answers").notNull(),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ 跨场景任务 ============
export const scenarios = mysqlTable("scenarios", {
  id: serial("id").primaryKey(),
  type: mysqlEnum("type", ["birthday_guard"]).notNull(),
  title: varchar("title", { length: 128 }).notNull(),
  contactId: bigint("contact_id", { mode: "number", unsigned: true }).notNull(),
  // JSON: { lockCents, birthday, gifts: [{kind, budgetCents, merchant, itemName}], orderLeadDays }
  payload: text("payload").notNull(),
  status: mysqlEnum("status", ["armed", "locked", "ordered", "done", "cancelled"])
    .notNull()
    .default("armed"),
  // JSON: [{ at, text }]
  log: text("log").notNull(),
  createdAt: timestamp("created_at").notNull().defaultNow(),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
});

// ============ 礼品订单（鲜花/蛋糕） ============
export const giftOrders = mysqlTable("gift_orders", {
  id: serial("id").primaryKey(),
  scenarioId: bigint("scenario_id", { mode: "number", unsigned: true }).notNull(),
  kind: mysqlEnum("kind", ["flower", "cake"]).notNull(),
  merchant: varchar("merchant", { length: 64 }).notNull(),
  itemName: varchar("item_name", { length: 128 }).notNull(),
  amountCents: int("amount_cents").notNull(),
  deliverAt: timestamp("deliver_at").notNull(),
  status: mysqlEnum("status", ["scheduled", "placed", "delivered", "cancelled"])
    .notNull()
    .default("scheduled"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ Agent 对话 ============
export const agentMessages = mysqlTable("agent_messages", {
  id: serial("id").primaryKey(),
  role: mysqlEnum("role", ["user", "assistant"]).notNull(),
  content: text("content").notNull(),
  // JSON: [{ name, summary }] 本轮触发的工具
  tools: text("tools"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});
