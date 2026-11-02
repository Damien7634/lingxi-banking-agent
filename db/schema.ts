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

// ============ 多币种钱包（跨境电商） ============
export const fxWallets = mysqlTable("fx_wallets", {
  id: serial("id").primaryKey(),
  currency: varchar("currency", { length: 3 }).notNull(), // USD/EUR/GBP/JPY/HKD
  // 外币余额，单位：外币最小单位（如美分）
  balanceForeignCents: int("balance_foreign_cents").notNull().default(0),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
});

// ============ 外汇牌价 ============
export const fxRates = mysqlTable("fx_rates", {
  id: serial("id").primaryKey(),
  pair: varchar("pair", { length: 10 }).notNull(), // USDCNY
  currency: varchar("currency", { length: 3 }).notNull(),
  // 1 外币 = rateBps/10000 人民币；buy=银行买入价（客户结汇），sell=银行卖出价（客户购汇）
  buyRateBps: int("buy_rate_bps").notNull(),
  sellRateBps: int("sell_rate_bps").notNull(),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
});

// ============ 外汇衍生品合约（远期锁汇/期权/掉期） ============
export const fxContracts = mysqlTable("fx_contracts", {
  id: serial("id").primaryKey(),
  type: mysqlEnum("type", ["forward", "option", "swap"]).notNull(),
  currency: varchar("currency", { length: 3 }).notNull(),
  // 名义外币本金，单位：外币最小单位
  foreignAmountCents: int("foreign_amount_cents").notNull(),
  // 锁定汇率，1 外币 = lockedRateBps/10000 人民币
  lockedRateBps: int("locked_rate_bps").notNull(),
  // 结汇=sell_foreign（卖外币收人民币），购汇=buy_foreign
  direction: mysqlEnum("direction", ["sell_foreign", "buy_foreign"])
    .notNull()
    .default("sell_foreign"),
  // 远期保证金（人民币分，名义本金等值的 5%）
  marginCents: int("margin_cents").notNull().default(0),
  // 期权费（人民币分，名义本金的 1.5%）
  premiumCents: int("premium_cents").notNull().default(0),
  expiresAt: timestamp("expires_at").notNull(),
  status: mysqlEnum("status", ["active", "settled", "cancelled"])
    .notNull()
    .default("active"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ 供应链金融产品 ============
export const scfProducts = mysqlTable("scf_products", {
  id: serial("id").primaryKey(),
  code: varchar("code", { length: 32 }).notNull(), // ORDER_LOAN / WAREHOUSE / TAX_REBATE / CREDIT_INS
  name: varchar("name", { length: 64 }).notNull(),
  // 年化利率，基点（万分之一）
  rateBps: int("rate_bps").notNull(),
  maxAmountCents: int("max_amount_cents").notNull(),
  termDays: int("term_days").notNull(),
  tags: varchar("tags", { length: 128 }),
  description: varchar("description", { length: 255 }),
});

// ============ 供应链融资（在途贷款） ============
export const scfLoans = mysqlTable("scf_loans", {
  id: serial("id").primaryKey(),
  productId: bigint("product_id", { mode: "number", unsigned: true }).notNull(),
  amountCents: int("amount_cents").notNull(),
  rateBps: int("rate_bps").notNull(),
  purpose: varchar("purpose", { length: 255 }),
  relatedRef: varchar("related_ref", { length: 64 }), // 关联平台订单号 / 报关单号
  status: mysqlEnum("status", ["disbursed", "repaid", "overdue"])
    .notNull()
    .default("disbursed"),
  appliedAt: timestamp("applied_at").notNull().defaultNow(),
  dueAt: timestamp("due_at").notNull(),
  repaidAt: timestamp("repaid_at"),
});

// ============ 物流运单（外贸一体化） ============
export const shipments = mysqlTable("shipments", {
  id: serial("id").primaryKey(),
  trackingNo: varchar("tracking_no", { length: 32 }).notNull(),
  carrier: varchar("carrier", { length: 64 }).notNull(), // 云途/递四方/燕文/顺丰国际
  platform: varchar("platform", { length: 32 }).notNull(), // Amazon 北美站 / 速卖通 / Temu / 独立站
  destination: varchar("destination", { length: 64 }).notNull(),
  goodsDesc: varchar("goods_desc", { length: 128 }),
  weightKg: varchar("weight_kg", { length: 16 }),
  freightCents: int("freight_cents").notNull().default(0),
  status: mysqlEnum("status", ["label_created", "in_transit", "customs_clearance", "delivered", "exception"])
    .notNull()
    .default("label_created"),
  // JSON: [{ time, text, location }]
  milestones: text("milestones").notNull(),
  etaAt: timestamp("eta_at"),
  createdAt: timestamp("created_at").notNull().defaultNow(),
});

// ============ 海关报关单 ============
export const customsDeclarations = mysqlTable("customs_declarations", {
  id: serial("id").primaryKey(),
  declNo: varchar("decl_no", { length: 32 }).notNull(),
  port: varchar("port", { length: 64 }).notNull(),
  goodsDesc: varchar("goods_desc", { length: 128 }),
  valueUsdCents: int("value_usd_cents").notNull(),
  status: mysqlEnum("status", ["declared", "inspecting", "released", "closed"])
    .notNull()
    .default("declared"),
  declaredAt: timestamp("declared_at").notNull().defaultNow(),
  releasedAt: timestamp("released_at"),
});

// ============ 出口退税 ============
export const taxRefunds = mysqlTable("tax_refunds", {
  id: serial("id").primaryKey(),
  period: varchar("period", { length: 8 }).notNull(), // 2025-09
  amountCents: int("amount_cents").notNull(),
  status: mysqlEnum("status", ["filing", "reviewing", "approved", "paid"])
    .notNull()
    .default("filing"),
  progress: int("progress").notNull().default(0), // 0-100
  declNo: varchar("decl_no", { length: 32 }),
  updatedAt: timestamp("updated_at").notNull().defaultNow(),
});
