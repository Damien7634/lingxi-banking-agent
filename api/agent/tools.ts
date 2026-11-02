import { z } from "zod";
import { tool } from "ai";
import { getDb } from "../queries/connection";
import { cards, contacts, holdings, wealthProducts, scenarios, transactions, fxWallets, fxRates, fxContracts, scfProducts, scfLoans, shipments, customsDeclarations, taxRefunds } from "@db/schema";
import { and, desc, eq, gte } from "drizzle-orm";
import { getMainAccount, availableCents, spend, receive, fenToYuan, lockCents, unlockCents } from "../lib/bank";
import { matchContacts } from "../routers/account";
import { recommendProducts } from "../routers/wealth";
import { toCnyCents, CURRENCY_META } from "../routers/fx";
import { detectAnomalies, CATEGORIES } from "../routers/bill";
import { tickBirthdayGuard, DEFAULT_GIFTS, parsePayload } from "../lib/scenarioEngine";
import { scheduledTransfers, aaSplits, riskProfiles, giftOrders } from "@db/schema";

const yuan = (n: number) => `¥${n.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
const toCents = (y: number) => Math.round(y * 100);

/** 解析收款人：唯一匹配直接用；多个候选让模型追问；没有则明确告知 */
async function resolvePayee(query: string) {
  const matched = await matchContacts(query);
  if (matched.length === 0) {
    return { error: `通讯录里没有找到与「${query}」相关的收款人，请先添加收款人` } as const;
  }
  if (matched.length > 1) {
    return {
      needClarify: true,
      candidates: matched.map((c) => ({
        id: c.id,
        name: c.name,
        phone: c.phone,
        bank: c.bankName,
        cardTail: c.cardTail,
        relation: c.relation,
      })),
      message: `找到 ${matched.length} 位可能的收款人，请用户确认是哪一位`,
    } as const;
  }
  return { contact: matched[0] } as const;
}

type CardRow = typeof cards.$inferSelect;
type ResolveCardResult =
  | { kind: "card"; card: CardRow }
  | { kind: "error"; error: string }
  | { kind: "multi"; cards: CardRow[]; message: string };

async function resolveCard(query?: string): Promise<ResolveCardResult> {
  const db = getDb();
  const all = await db.query.cards.findMany();
  if (all.length === 0) return { kind: "error", error: "当前没有绑定任何银行卡" };
  if (!query) {
    if (all.length === 1) return { kind: "card", card: all[0]! };
    return { kind: "multi", cards: all, message: "有多张卡，请说明尾号或类型" };
  }
  const hit = all.find(
    (c) =>
      c.last4 === query.replace(/\D/g, "").slice(-4) ||
      (query.includes("信用") && c.cardType === "credit") ||
      ((query.includes("借记") || query.includes("储蓄")) && c.cardType === "debit"),
  );
  if (!hit) return { kind: "error", error: `没有匹配「${query}」的卡片` };
  return { kind: "card", card: hit };
}

export function buildAgentTools() {
  return {
    getAccountOverview: tool({
      description: "查询账户总览：总余额、可用余额、锁定金额、近期交易、待执行定时转账",
      inputSchema: z.object({}),
      execute: async () => {
        const db = getDb();
        const acc = await getMainAccount();
        const recent = await db.query.transactions.findMany({ orderBy: [desc(transactions.occurredAt)], limit: 5 });
        return {
          总余额: yuan(acc.balanceCents / 100),
          可用余额: yuan(availableCents(acc) / 100),
          场景锁定: yuan(acc.lockedCents / 100),
          近期交易: recent.map((t) => `${t.occurredAt.toISOString().slice(0, 10)} ${t.direction === "in" ? "+" : "-"}${yuan(t.amountCents / 100)} ${t.counterparty}（${t.category}）`),
        };
      },
    }),

    searchPayee: tool({
      description: "按人名/手机号/备注/关系搜索收款人，返回候选列表",
      inputSchema: z.object({ query: z.string().describe("人名、手机号、备注或关系，如「李娜」「13800138000」「老婆」") }),
      execute: async ({ query }) => {
        const matched = await matchContacts(query);
        return matched.map((c) => ({ id: c.id, 姓名: c.name, 手机: c.phone, 银行: c.bankName, 卡尾号: c.cardTail, 关系: c.relation, 备注: c.note }));
      },
    }),

    transfer: tool({
      description: "向收款人实时转账。收款人可以是人名/手机号/备注，系统自动识别；若匹配多人会返回候选请用户确认",
      inputSchema: z.object({
        payeeQuery: z.string().describe("收款人的人名/手机号/备注"),
        amountYuan: z.number().positive().describe("转账金额（元）"),
        memo: z.string().optional().describe("转账备注"),
      }),
      execute: async ({ payeeQuery, amountYuan, memo }) => {
        const r = await resolvePayee(payeeQuery);
        if ("error" in r) return r;
        if ("needClarify" in r) return r;
        try {
          const res = await spend({
            amountCents: toCents(amountYuan),
            category: "转账",
            counterparty: r.contact.name,
            memo: memo ?? `转账给${r.contact.name}`,
          });
          return {
            成功: true,
            收款人: `${r.contact.name}（${r.contact.bankName} 尾号${r.contact.cardTail}）`,
            金额: yuan(amountYuan),
            流水号: res.txId,
            余额: yuan(res.balanceCents / 100),
          };
        } catch (e) {
          return { 成功: false, 原因: (e as Error).message };
        }
      },
    }),

    scheduleTransfer: tool({
      description: "创建定时转账，在指定日期自动执行",
      inputSchema: z.object({
        payeeQuery: z.string(),
        amountYuan: z.number().positive(),
        executeAt: z.string().describe("执行时间，ISO 格式，如 2026-11-05T09:00:00+08:00"),
        memo: z.string().optional(),
      }),
      execute: async ({ payeeQuery, amountYuan, executeAt, memo }) => {
        const r = await resolvePayee(payeeQuery);
        if ("error" in r) return r;
        if ("needClarify" in r) return r;
        const at = new Date(executeAt);
        if (Number.isNaN(at.getTime())) return { 成功: false, 原因: "无法解析执行时间" };
        if (at.getTime() < Date.now()) return { 成功: false, 原因: "执行时间必须晚于当前时间" };
        const [{ id }] = await getDb()
          .insert(scheduledTransfers)
          .values({ contactId: r.contact.id, amountCents: toCents(amountYuan), memo, executeAt: at })
          .$returningId();
        return { 成功: true, 任务号: id, 收款人: r.contact.name, 金额: yuan(amountYuan), 执行时间: at.toISOString() };
      },
    }),

    createAASplit: tool({
      description: "创建 AA 收款：把一笔总账单按人数均摊，向参与者发起收款",
      inputSchema: z.object({
        title: z.string().describe("如「部门聚餐·海底捞」"),
        totalYuan: z.number().positive(),
        participantNames: z.array(z.string()).min(1).describe("参与 AA 的人名列表（不含发起人）"),
      }),
      execute: async ({ title, totalYuan, participantNames }) => {
        const total = toCents(totalYuan);
        const per = Math.floor(total / (participantNames.length + 1));
        const list = participantNames.map((name, i) => ({
          name,
          phone: "",
          amountCents: i === 0 ? total - per * participantNames.length : per,
          paid: false,
        }));
        const [{ id }] = await getDb()
          .insert(aaSplits)
          .values({ title, totalCents: total, participants: JSON.stringify(list) })
          .$returningId();
        return {
          成功: true,
          收款单号: id,
          每人应付: list.map((p) => `${p.name} ${yuan(p.amountCents / 100)}`),
          说明: `已向 ${participantNames.length} 人发起收款，合计 ${yuan(totalYuan)}`,
        };
      },
    }),

    getMonthlyBill: tool({
      description: "月度账单分析：收支总额、分类占比、大额商户、环比",
      inputSchema: z.object({
        year: z.number().int(),
        month: z.number().int().min(1).max(12),
      }),
      execute: async ({ year, month }) => {
        const start = new Date(year, month - 1, 1);
        const end = new Date(year, month, 1);
        const rows = await getDb().query.transactions.findMany({
          where: and(gte(transactions.occurredAt, start)),
        });
        const inMonth = rows.filter((t) => t.occurredAt >= start && t.occurredAt < end);
        let totalIn = 0, totalOut = 0;
        const byCat = new Map<string, number>();
        for (const t of inMonth) {
          if (t.direction === "in") totalIn += t.amountCents;
          else {
            totalOut += t.amountCents;
            byCat.set(t.category, (byCat.get(t.category) ?? 0) + t.amountCents);
          }
        }
        const top = [...byCat.entries()].sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k}: ${yuan(v / 100)}（${totalOut ? ((v / totalOut) * 100).toFixed(1) : 0}%）`);
        return { 月份: `${year}-${String(month).padStart(2, "0")}`, 收入: yuan(totalIn / 100), 支出: yuan(totalOut / 100), 笔数: inMonth.length, 分类排行: top, 可选分类: [...CATEGORIES] };
      },
    }),

    detectAnomalies: tool({
      description: "扫描近期交易中的异常/可疑交易（夜间大额、陌生对手方、高频异常），用于安全提醒",
      inputSchema: z.object({ days: z.number().int().min(7).max(365).default(90) }),
      execute: async ({ days }) => {
        const flagged = await detectAnomalies(days);
        return flagged.map((f) => ({
          时间: f.tx.occurredAt.toISOString(),
          对手方: f.tx.counterparty,
          金额: yuan(f.tx.amountCents / 100),
          原因: f.reasons,
        }));
      },
    }),

    getRiskProfile: tool({
      description: "查询用户最近一次理财风险测评结果",
      inputSchema: z.object({}),
      execute: async () => {
        const rows = await getDb().query.riskProfiles.findMany({ orderBy: [desc(riskProfiles.createdAt)], limit: 1 });
        return rows[0] ?? { message: "尚未做过风险测评" };
      },
    }),

    submitRiskAssessment: tool({
      description: "提交风险测评结果。score 0-100，系统换算为保守/稳健/平衡/进取/激进",
      inputSchema: z.object({
        score: z.number().int().min(0).max(100),
        answers: z.record(z.string(), z.string()).describe("问题到答案的映射"),
      }),
      execute: async ({ score, answers }) => {
        const level = score <= 30 ? "保守型" : score <= 50 ? "稳健型" : score <= 70 ? "平衡型" : score <= 85 ? "进取型" : "激进型";
        await getDb().insert(riskProfiles).values({ score, level, answers: JSON.stringify(answers) });
        return { 成功: true, 测评结果: level, 得分: score };
      },
    }),

    recommendWealth: tool({
      description: "根据用户风险测评结果推荐理财产品（含对比：收益率、风险等级、起购金额、期限）",
      inputSchema: z.object({}),
      execute: async () => {
        const { profile, maxRisk, products } = await recommendProducts(4);
        return {
          风险测评: profile ? `${profile.level}（得分 ${profile.score}）` : "未测评，按稳健型保守推荐",
          可买上限风险等级: maxRisk,
          推荐: products.map((p) => ({
            名称: p.name,
            类型: p.type,
            风险: p.riskLevel,
            业绩基准: `${(p.annualRateBps / 100).toFixed(2)}%`,
            起购: yuan(p.minAmountCents / 100),
            期限: p.termDays === 0 ? "灵活申赎" : `${p.termDays} 天`,
            特点: p.tags,
            说明: p.description,
          })),
        };
      },
    }),

    purchaseWealth: tool({
      description: "申购理财产品。会做适当性校验（风险等级不可超过测评结果）和余额校验",
      inputSchema: z.object({
        productName: z.string().describe("产品名（可模糊，如「日日盈」）"),
        amountYuan: z.number().positive(),
      }),
      execute: async ({ productName, amountYuan }) => {
        const db = getDb();
        const all = await db.query.wealthProducts.findMany();
        const product = all.find((p) => p.name.includes(productName)) ?? null;
        if (!product) return { 成功: false, 原因: `没有找到与「${productName}」匹配的产品`, 可选: all.map((p) => p.name) };
        const profile = (await recommendProducts(4));
        const ORDER = ["R1", "R2", "R3", "R4", "R5"];
        if (ORDER.indexOf(product.riskLevel) > ORDER.indexOf(profile.maxRisk)) {
          return {
            成功: false,
            原因: `适当性拦截：该产品风险等级 ${product.riskLevel}，超出您的风险承受能力（当前 ${profile.profile?.level ?? "稳健型"}，上限 ${profile.maxRisk}）。如需购买请先重新测评。`,
          };
        }
        if (toCents(amountYuan) < product.minAmountCents) {
          return { 成功: false, 原因: `起购金额 ${yuan(product.minAmountCents / 100)}` };
        }
        try {
          const r = await spend({ amountCents: toCents(amountYuan), category: "理财", counterparty: "灵犀理财", memo: `申购 ${product.name}` });
          const existing = await db.query.holdings.findFirst({ where: and(eq(holdings.productId, product.id), eq(holdings.status, "holding")) });
          if (existing) {
            await db.update(holdings).set({ amountCents: existing.amountCents + toCents(amountYuan) }).where(eq(holdings.id, existing.id));
          } else {
            await db.insert(holdings).values({ productId: product.id, amountCents: toCents(amountYuan) });
          }
          return { 成功: true, 产品: product.name, 金额: yuan(amountYuan), 账户余额: yuan(r.balanceCents / 100) };
        } catch (e) {
          return { 成功: false, 原因: (e as Error).message };
        }
      },
    }),

    redeemWealth: tool({
      description: "赎回理财持仓（默认全额赎回，含收益）",
      inputSchema: z.object({
        productName: z.string().describe("产品名（可模糊）"),
        amountYuan: z.number().positive().optional().describe("赎回金额，缺省为全额赎回"),
      }),
      execute: async ({ productName, amountYuan }) => {
        const db = getDb();
        const rows = await db.query.holdings.findMany({ where: eq(holdings.status, "holding") });
        const withProduct = await Promise.all(rows.map(async (h) => ({ h, p: await db.query.wealthProducts.findFirst({ where: eq(wealthProducts.id, h.productId) }) })));
        const hit = withProduct.find((x) => x.p?.name.includes(productName));
        if (!hit) return { 成功: false, 原因: `没有持有与「${productName}」匹配的产品`, 当前持仓: withProduct.map((x) => `${x.p?.name} ${yuan(x.h.amountCents / 100)}`) };
        const amount = Math.min(amountYuan ? toCents(amountYuan) : hit.h.amountCents, hit.h.amountCents);
        const full = amount >= hit.h.amountCents;
        await receive({ amountCents: amount + (full ? hit.h.profitCents : 0), category: "理财", counterparty: "灵犀理财", memo: `赎回 ${hit.p?.name}` });
        await db.update(holdings).set(full ? { status: "redeemed", redeemedAt: new Date(), amountCents: 0 } : { amountCents: hit.h.amountCents - amount }).where(eq(holdings.id, hit.h.id));
        return { 成功: true, 产品: hit.p?.name, 赎回: yuan(amount / 100), 收益到账: full ? yuan(hit.h.profitCents / 100) : "部分赎回收益按比例结转" };
      },
    }),

    listCards: tool({
      description: "查询名下银行卡：类型、尾号、额度、状态、交易限制",
      inputSchema: z.object({}),
      execute: async () => {
        const all = await getDb().query.cards.findMany();
        const statusText = { normal: "正常", frozen: "已冻结", lost: "已挂失", restricted: "交易受限" } as const;
        return all.map((c) => ({
          卡片: `${c.bankName}${c.cardType === "credit" ? "信用卡" : "借记卡"} 尾号${c.last4}`,
          状态: statusText[c.status],
          信用额度: c.creditLimitCents ? yuan(c.creditLimitCents / 100) : null,
          已用额度: c.cardType === "credit" ? yuan(c.usedCents / 100) : null,
          单日限额: yuan(c.dailyLimitCents / 100),
          线上支付: c.onlinePayEnabled ? "已开启" : "已关闭",
        }));
      },
    }),

    applyCard: tool({
      description: "申请新卡（借记卡/信用卡）",
      inputSchema: z.object({ cardType: z.enum(["debit", "credit"]) }),
      execute: async ({ cardType }) => {
        const last4 = String(Math.floor(1000 + Math.random() * 9000));
        await getDb().insert(cards).values({
          cardType,
          bankName: "灵犀银行",
          last4,
          creditLimitCents: cardType === "credit" ? 3000000 : null,
          status: "normal",
        });
        return { 成功: true, 卡种: cardType === "credit" ? "信用卡" : "借记卡", 卡尾号: last4, 说明: cardType === "credit" ? "预审通过，初始额度 ¥30,000，实体卡 3 个工作日内寄出" : "开卡成功，可立即使用" };
      },
    }),

    manageCard: tool({
      description: "卡片管理：挂失/冻结/解冻/限制交易/解除限制/调整额度/开关线上支付",
      inputSchema: z.object({
        cardQuery: z.string().optional().describe("卡尾号或「信用卡」「借记卡」"),
        action: z.enum(["freeze", "unfreeze", "reportLost", "restrict", "unrestrict", "setDailyLimit", "setCreditLimit", "toggleOnlinePay"]),
        amountYuan: z.number().positive().optional().describe("调整额度时的目标金额（元）"),
        enabled: z.boolean().optional().describe("开关线上支付时的目标状态"),
      }),
      execute: async ({ cardQuery, action, amountYuan, enabled }) => {
        const db = getDb();
        const r = await resolveCard(cardQuery);
        if (r.kind === "error") return { 成功: false, 原因: r.error };
        if (r.kind === "multi") return { 成功: false, 原因: r.message, 卡片列表: r.cards.map((c) => `${c.cardType === "credit" ? "信用卡" : "借记卡"} 尾号${c.last4}`) };
        const card = r.card;
        const statusMap = { freeze: "frozen", unfreeze: "normal", reportLost: "lost", restrict: "restricted", unrestrict: "normal" } as const;
        if (action in statusMap) {
          if (card.status === "lost") return { 成功: false, 原因: "该卡已挂失，如需用卡请补办" };
          await db.update(cards).set({ status: statusMap[action as keyof typeof statusMap] }).where(eq(cards.id, card.id));
          const label = { freeze: "临时冻结", unfreeze: "解除冻结", reportLost: "挂失", restrict: "限制交易", unrestrict: "解除交易限制" }[action as keyof typeof statusMap];
          return { 成功: true, 卡片: `尾号${card.last4}`, 操作: label };
        }
        if (action === "setDailyLimit" || action === "setCreditLimit") {
          if (!amountYuan) return { 成功: false, 原因: "请提供目标额度金额" };
          if (action === "setCreditLimit" && card.cardType !== "credit") return { 成功: false, 原因: "借记卡没有信用额度，可调整单日限额" };
          await db.update(cards).set(action === "setCreditLimit" ? { creditLimitCents: toCents(amountYuan) } : { dailyLimitCents: toCents(amountYuan) }).where(eq(cards.id, card.id));
          return { 成功: true, 卡片: `尾号${card.last4}`, 新额度: yuan(amountYuan) };
        }
        if (action === "toggleOnlinePay") {
          await db.update(cards).set({ onlinePayEnabled: enabled ?? !card.onlinePayEnabled }).where(eq(cards.id, card.id));
          return { 成功: true, 卡片: `尾号${card.last4}`, 线上支付: (enabled ?? !card.onlinePayEnabled) ? "已开启" : "已关闭" };
        }
        return { 成功: false, 原因: "不支持的操作" };
      },
    }),

    setupBirthdayGuard: tool({
      description: "创建生日守护跨场景任务：自动读取联系人生日 → 生日当月锁定指定金额活期 → 生日前 N 天自动下单鲜花和蛋糕 → 生日当天解锁余款",
      inputSchema: z.object({
        contactQuery: z.string().describe("联系人（人名/备注/关系，如「我爱人」「李娜」「老婆」）"),
        lockYuan: z.number().positive().default(1000).describe("当月锁定的活期金额，默认 1000"),
        orderLeadDays: z.number().int().min(1).max(7).default(2).describe("生日前几天下单礼品，默认 2"),
      }),
      execute: async ({ contactQuery, lockYuan, orderLeadDays }) => {
        const db = getDb();
        const matched = await matchContacts(contactQuery);
        if (matched.length === 0) return { 成功: false, 原因: `没有找到与「${contactQuery}」相关的联系人` };
        if (matched.length > 1) return { needClarify: true, candidates: matched.map((c) => `${c.name}（${c.relation}）`) };
        const contact = matched[0];
        if (!contact.birthday) return { 成功: false, 原因: `「${contact.name}」还没有生日信息，请先补充` };
        const existing = await db.query.scenarios.findMany({ where: eq(scenarios.contactId, contact.id) });
        if (existing.some((s) => !["done", "cancelled"].includes(s.status))) {
          return { 成功: false, 原因: `「${contact.name}」已有进行中的生日守护任务` };
        }
        const lockCents = toCents(lockYuan);
        const acc = await getMainAccount();
        if (availableCents(acc) < lockCents) return { 成功: false, 原因: `可用余额不足：可用 ${yuan(availableCents(acc) / 100)}，需锁定 ${yuan(lockYuan)}` };
        const payload = { lockCents, birthday: contact.birthday, orderLeadDays, gifts: DEFAULT_GIFTS };
        const [{ id }] = await db.insert(scenarios).values({
          type: "birthday_guard",
          title: `${contact.name}生日守护`,
          contactId: contact.id,
          payload: JSON.stringify(payload),
          status: "armed",
          log: JSON.stringify([{ at: new Date().toISOString(), text: `守护已开启：${contact.name}生日 ${contact.birthday}，当月锁定 ${yuan(lockYuan)}，生日前 ${orderLeadDays} 天自动订鲜花蛋糕` }]),
        }).$returningId();
        const events = await tickBirthdayGuard();
        const giftText = DEFAULT_GIFTS.map((g) => `${g.merchant}「${g.itemName}」${yuan(g.amountCents / 100)}`).join(" + ");
        return {
          成功: true,
          任务号: id,
          联系人: `${contact.name}（生日 ${contact.birthday}）`,
          计划: [`生日当月：锁定活期 ${yuan(lockYuan)}`, `生日前 ${orderLeadDays} 天：自动下单 ${giftText}`, `生日当天：礼品送达，余款解锁`],
          已触发: events,
        };
      },
    }),

    listScenarios: tool({
      description: "查询所有跨场景守护任务及礼品订单状态",
      inputSchema: z.object({}),
      execute: async () => {
        await tickBirthdayGuard();
        const db = getDb();
        const rows = await db.query.scenarios.findMany({ orderBy: [desc(scenarios.createdAt)] });
        return Promise.all(rows.map(async (r) => {
          const contact = await db.query.contacts.findFirst({ where: eq(contacts.id, r.contactId) });
          const orders = await db.query.giftOrders.findMany({ where: eq(giftOrders.scenarioId, r.id) });
          const p = parsePayload(r);
          const statusText = { armed: "已布防", locked: "已锁定资金", ordered: "礼品已下单", done: "已完成", cancelled: "已取消" } as const;
          return {
            任务: r.title,
            状态: statusText[r.status],
            生日: p.birthday,
            锁定: yuan(p.lockCents / 100),
            联系人: contact?.name,
            订单: orders.map((o) => `${o.merchant}「${o.itemName}」${yuan(o.amountCents / 100)}（${o.status}）`),
            日志: (JSON.parse(r.log) as Array<{ text: string }>).map((l) => l.text),
          };
        }));
      },
    }),

    // ================= 跨境电商：外汇与避险 =================
    getFxRates: tool({
      description: "查询实时外汇牌价（美元/欧元/英镑/港币/日元兑人民币的结汇价与购汇价）",
      inputSchema: z.object({}),
      execute: async () => {
        const rows = await getDb().query.fxRates.findMany();
        return rows.map((r) => ({
          币种: `${CURRENCY_META[r.currency]?.name ?? r.currency} ${r.currency}`,
          结汇价: (r.buyRateBps / 10000).toFixed(4),
          购汇价: (r.sellRateBps / 10000).toFixed(4),
        }));
      },
    }),

    getFxWallets: tool({
      description: "查询多币种钱包余额及折合人民币",
      inputSchema: z.object({}),
      execute: async () => {
        const db = getDb();
        const [wallets, rates] = await Promise.all([db.query.fxWallets.findMany(), db.query.fxRates.findMany()]);
        const rateMap = Object.fromEntries(rates.map((r) => [r.currency, r.buyRateBps]));
        return wallets.map((w) => ({
          币种: `${CURRENCY_META[w.currency]?.name ?? w.currency} ${w.currency}`,
          外币余额: `${CURRENCY_META[w.currency]?.symbol ?? ""}${(w.balanceForeignCents / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 })}`,
          折合人民币: yuan(toCnyCents(w.balanceForeignCents, rateMap[w.currency] ?? 0)),
        }));
      },
    }),

    getFxExposure: tool({
      description: "分析汇率风险敞口：各币种资产折合人民币、已对冲比例、未对冲敞口与建议",
      inputSchema: z.object({}),
      execute: async () => {
        const db = getDb();
        const [wallets, rates, contracts] = await Promise.all([
          db.query.fxWallets.findMany(),
          db.query.fxRates.findMany(),
          db.query.fxContracts.findMany({ where: eq(fxContracts.status, "active") }),
        ]);
        const rateMap = Object.fromEntries(rates.map((r) => [r.currency, r.buyRateBps]));
        const items = wallets.filter((w) => w.balanceForeignCents > 0).map((w) => {
          const cnyCents = toCnyCents(w.balanceForeignCents, rateMap[w.currency] ?? 0);
          const hedged = contracts.filter((c) => c.currency === w.currency).reduce((s, c) => s + toCnyCents(c.foreignAmountCents, rateMap[c.currency] ?? 0), 0);
          return {
            币种: w.currency,
            外币余额: (w.balanceForeignCents / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2 }),
            折合人民币: yuan(cnyCents),
            已对冲: yuan(hedged),
            对冲比例: `${Math.round(Math.min(1, hedged / cnyCents) * 100)}%`,
          };
        });
        const total = items.reduce((s, i) => s + toCnyCents(wallets.find((w) => w.currency === i.币种)!.balanceForeignCents, rateMap[i.币种] ?? 0), 0);
        return {
          敞口明细: items,
          敞口合计: yuan(total),
          提示: "未对冲部分在人民币贬值时产生汇兑收益、升值时产生汇兑损失；建议对 1-3 个月内确定回款的敞口做远期锁汇，不确定时点部分用期权保留弹性",
        };
      },
    }),

    lockForwardRate: tool({
      description: "远期锁汇（远期结售汇）：为外币资金锁定未来交割汇率，冻结 5% 人民币保证金",
      inputSchema: z.object({
        currency: z.enum(["USD", "EUR", "GBP", "HKD", "JPY"]).describe("外币币种"),
        foreignAmount: z.number().positive().describe("外币金额（元，如 50000 表示 5 万美元）"),
        tenorDays: z.number().int().min(7).max(365).describe("锁定天数，如 30"),
      }),
      execute: async ({ currency, foreignAmount, tenorDays }) => {
        const db = getDb();
        const rateRow = await db.query.fxRates.findFirst({ where: eq(fxRates.currency, currency) });
        if (!rateRow) return { 成功: false, 原因: "暂不支持该币种" };
        const foreignCents = toCents(foreignAmount);
        const wallet = await db.query.fxWallets.findFirst({ where: eq(fxWallets.currency, currency) });
        if (!wallet || wallet.balanceForeignCents < foreignCents) {
          return { 成功: false, 原因: `${currency} 钱包余额不足（当前 ${((wallet?.balanceForeignCents ?? 0) / 100).toLocaleString()}），无法锁定该金额远期结汇` };
        }
        const rateBps = rateRow.buyRateBps;
        const marginCents = Math.round(toCnyCents(foreignCents, rateBps) * 0.05);
        const acc = await getMainAccount();
        if (availableCents(acc) < marginCents) return { 成功: false, 原因: `活期可用余额不足支付保证金 ${yuan(marginCents)}` };
        await lockCents(marginCents);
        const expiresAt = new Date(Date.now() + tenorDays * 86400000);
        const [{ id }] = await db.insert(fxContracts).values({
          type: "forward", currency, foreignAmountCents: foreignCents, lockedRateBps: rateBps,
          direction: "sell_foreign", marginCents, expiresAt,
        }).$returningId();
        return {
          成功: true, 合约号: id, 币种: currency,
          名义本金: `${CURRENCY_META[currency]?.symbol ?? ""}${foreignAmount.toLocaleString()}`,
          锁定汇率: (rateBps / 10000).toFixed(4),
          冻结保证金: yuan(marginCents),
          到期日: expiresAt.toISOString().slice(0, 10),
          说明: `无论届时市场汇率如何波动，到期均按 ${(rateBps / 10000).toFixed(4)} 结汇`,
        };
      },
    }),

    buyFxOption: tool({
      description: "购买外汇期权：支付 1.5% 期权费，获得到期按约定汇率结汇的权利（不必须行权），适合不确定回款时点的敞口",
      inputSchema: z.object({
        currency: z.enum(["USD", "EUR", "GBP", "HKD", "JPY"]),
        foreignAmount: z.number().positive().describe("名义外币本金（元）"),
        tenorDays: z.number().int().min(7).max(365),
      }),
      execute: async ({ currency, foreignAmount, tenorDays }) => {
        const db = getDb();
        const rateRow = await db.query.fxRates.findFirst({ where: eq(fxRates.currency, currency) });
        if (!rateRow) return { 成功: false, 原因: "暂不支持该币种" };
        const foreignCents = toCents(foreignAmount);
        const rateBps = rateRow.buyRateBps;
        const premiumCents = Math.round(toCnyCents(foreignCents, rateBps) * 0.015);
        const acc = await getMainAccount();
        if (availableCents(acc) < premiumCents) return { 成功: false, 原因: `活期可用余额不足支付期权费 ${yuan(premiumCents)}` };
        await spend({ amountCents: premiumCents, category: "金融衍生品", counterparty: "灵犀银行金融市场部", memo: `${currency} 外汇期权费` });
        const expiresAt = new Date(Date.now() + tenorDays * 86400000);
        const [{ id }] = await db.insert(fxContracts).values({
          type: "option", currency, foreignAmountCents: foreignCents, lockedRateBps: rateBps,
          direction: "sell_foreign", premiumCents, expiresAt,
        }).$returningId();
        return {
          成功: true, 合约号: id, 类型: "外汇看跌期权（结汇保护）",
          执行汇率: (rateBps / 10000).toFixed(4),
          期权费: yuan(premiumCents),
          到期日: expiresAt.toISOString().slice(0, 10),
          说明: "若到期汇率低于执行价则行权保值，高于执行价可放弃行权按市价结汇（损失限于期权费）",
        };
      },
    }),

    listFxContracts: tool({
      description: "查询我的外汇衍生品合约（远期/期权）及状态",
      inputSchema: z.object({}),
      execute: async () => {
        const rows = await getDb().query.fxContracts.findMany({ orderBy: [desc(fxContracts.id)] });
        const typeText = { forward: "远期锁汇", option: "外汇期权", swap: "外汇掉期" } as const;
        const statusText = { active: "存续中", settled: "已交割", cancelled: "已取消" } as const;
        return rows.map((c) => ({
          合约号: c.id, 类型: typeText[c.type], 币种: c.currency,
          名义本金: (c.foreignAmountCents / 100).toLocaleString(),
          锁定汇率: (c.lockedRateBps / 10000).toFixed(4),
          保证金: c.marginCents > 0 ? yuan(c.marginCents) : "-",
          期权费: c.premiumCents > 0 ? yuan(c.premiumCents) : "-",
          到期日: c.expiresAt.toISOString().slice(0, 10),
          状态: statusText[c.status],
        }));
      },
    }),

    // ================= 跨境电商：供应链金融 =================
    listScfProducts: tool({
      description: "查询供应链金融产品（跨境订单贷/海外仓仓单质押贷/出口退税贷/出口信保融资）",
      inputSchema: z.object({}),
      execute: async () => {
        const rows = await getDb().query.scfProducts.findMany();
        return rows.map((p) => ({
          产品: p.name, 年化利率: `${(p.rateBps / 100).toFixed(2)}%`,
          额度上限: yuan(p.maxAmountCents), 期限: `${p.termDays} 天`,
          特点: p.tags?.split(","), 说明: p.description,
        }));
      },
    }),

    applyScfLoan: tool({
      description: "申请供应链金融融资（演示环境即时审批放款到活期）",
      inputSchema: z.object({
        productName: z.string().describe("产品名关键词，如「订单贷」「仓单」「退税」「信保」"),
        amountYuan: z.number().positive().describe("申请金额（元）"),
        purpose: z.string().optional().describe("用途，如「黑五备货」"),
        relatedRef: z.string().optional().describe("关联订单号/报关单号/退税期"),
      }),
      execute: async ({ productName, amountYuan, purpose, relatedRef }) => {
        const db = getDb();
        const all = await db.query.scfProducts.findMany();
        const p = all.find((x) => x.name.includes(productName)) ?? all.find((x) => x.code.includes(productName.toUpperCase()));
        if (!p) return { 成功: false, 原因: `未找到匹配「${productName}」的产品，可选：${all.map((x) => x.name).join("、")}` };
        const amountCents = toCents(amountYuan);
        if (amountCents > p.maxAmountCents) return { 成功: false, 原因: `超出额度上限 ${yuan(p.maxAmountCents)}` };
        const existing = (await db.query.scfLoans.findMany({ where: eq(scfLoans.productId, p.id) })).find((l) => l.status === "disbursed");
        if (existing) return { 成功: false, 原因: `「${p.name}」有在途融资 ${yuan(existing.amountCents)}，需先归还` };
        const dueAt = new Date(Date.now() + p.termDays * 86400000);
        const [{ id }] = await db.insert(scfLoans).values({
          productId: p.id, amountCents, rateBps: p.rateBps, purpose, relatedRef, status: "disbursed", dueAt,
        }).$returningId();
        await receive({ amountCents, category: "融资放款", counterparty: "灵犀银行供应链金融", memo: `${p.name} 放款` });
        return {
          成功: true, 融资号: id, 产品: p.name,
          放款金额: yuan(amountCents), 年化利率: `${(p.rateBps / 100).toFixed(2)}%`,
          到期日: dueAt.toISOString().slice(0, 10),
          说明: "资金已实时到账活期账户，可在首页余额中查看",
        };
      },
    }),

    repayScfLoan: tool({
      description: "归还供应链融资（本金+按天计息，从活期扣）",
      inputSchema: z.object({ productName: z.string().optional().describe("产品名关键词；不填则还最早一笔在途融资") }),
      execute: async ({ productName }) => {
        const db = getDb();
        const loans = await db.query.scfLoans.findMany({ where: eq(scfLoans.status, "disbursed"), orderBy: [desc(scfLoans.id)] });
        if (loans.length === 0) return { 成功: false, 原因: "当前没有在途融资" };
        let loan = loans[0]!;
        if (productName) {
          const products = await db.query.scfProducts.findMany();
          const p = products.find((x) => x.name.includes(productName));
          const hit = loans.find((l) => l.productId === p?.id);
          if (!hit) return { 成功: false, 原因: `没有匹配「${productName}」的在途融资` };
          loan = hit;
        }
        const p = await db.query.scfProducts.findFirst({ where: eq(scfProducts.id, loan.productId) });
        const days = Math.max(1, Math.ceil((Date.now() - loan.appliedAt.getTime()) / 86400000));
        const interestCents = Math.round((loan.amountCents * loan.rateBps * days) / 10000 / 365);
        const totalCents = loan.amountCents + interestCents;
        const acc = await getMainAccount();
        if (availableCents(acc) < totalCents) return { 成功: false, 原因: `可用余额不足：需 ${yuan(totalCents)}（本金 ${yuan(loan.amountCents)} + 利息 ${yuan(interestCents)}），当前可用 ${yuan(availableCents(acc))}` };
        await spend({ amountCents: totalCents, category: "融资还款", counterparty: "灵犀银行供应链金融", memo: `归还「${p?.name}」本金+利息` });
        await db.update(scfLoans).set({ status: "repaid", repaidAt: new Date() }).where(eq(scfLoans.id, loan.id));
        return { 成功: true, 产品: p?.name, 本金: yuan(loan.amountCents), 利息: yuan(interestCents), 合计扣款: yuan(totalCents) };
      },
    }),

    listScfLoans: tool({
      description: "查询我的供应链融资记录（在途/已结清）",
      inputSchema: z.object({}),
      execute: async () => {
        const db = getDb();
        const loans = await db.query.scfLoans.findMany({ orderBy: [desc(scfLoans.id)] });
        const products = await db.query.scfProducts.findMany();
        const pmap = Object.fromEntries(products.map((p) => [p.id, p.name]));
        const statusText = { disbursed: "在途", repaid: "已结清", overdue: "逾期" } as const;
        return loans.map((l) => ({
          产品: pmap[l.productId], 金额: yuan(l.amountCents),
          年化利率: `${(l.rateBps / 100).toFixed(2)}%`,
          用途: l.purpose, 状态: statusText[l.status],
          申请日: l.appliedAt.toISOString().slice(0, 10), 到期日: l.dueAt.toISOString().slice(0, 10),
        }));
      },
    }),

    // ================= 跨境电商：外贸一体化（物流/海关/退税） =================
    listShipments: tool({
      description: "查询外贸物流运单列表（承运商/平台/目的地/状态/最新轨迹）",
      inputSchema: z.object({}),
      execute: async () => {
        const rows = await getDb().query.shipments.findMany({ orderBy: [desc(shipments.id)] });
        const statusText = { label_created: "已建单待揽收", in_transit: "运输中", customs_clearance: "清关中", delivered: "已签收", exception: "异常" } as const;
        return rows.map((s) => {
          const ms = JSON.parse(s.milestones) as Array<{ time: string; text: string; location: string }>;
          return {
            单号: s.trackingNo, 承运商: s.carrier, 平台: s.platform, 目的地: s.destination,
            货物: s.goodsDesc, 运费: yuan(s.freightCents), 状态: statusText[s.status],
            最新轨迹: ms.length > 0 ? `${ms[ms.length - 1]!.time} ${ms[ms.length - 1]!.text}（${ms[ms.length - 1]!.location}）` : "-",
          };
        });
      },
    }),

    trackShipment: tool({
      description: "按运单号查询物流完整轨迹",
      inputSchema: z.object({ trackingNo: z.string().describe("运单号，支持部分单号模糊查询") }),
      execute: async ({ trackingNo }) => {
        const rows = await getDb().query.shipments.findMany();
        const kw = trackingNo.trim().toUpperCase();
        const hit = rows.find((s) => s.trackingNo.toUpperCase() === kw) ?? rows.find((s) => s.trackingNo.toUpperCase().includes(kw));
        if (!hit) return { 找到: false, 提示: `未找到运单「${trackingNo}」，当前运单号：${rows.map((r) => r.trackingNo).join("、")}` };
        const ms = JSON.parse(hit.milestones) as Array<{ time: string; text: string; location: string }>;
        return {
          找到: true, 单号: hit.trackingNo, 承运商: hit.carrier, 目的地: hit.destination,
          预计到达: hit.etaAt?.toISOString().slice(0, 10),
          轨迹: ms.map((m) => `${m.time}｜${m.text}｜${m.location}`),
        };
      },
    }),

    listCustoms: tool({
      description: "查询海关报关单状态（申报/查验/放行）及关联退税",
      inputSchema: z.object({}),
      execute: async () => {
        const db = getDb();
        const decls = await db.query.customsDeclarations.findMany({ orderBy: [desc(customsDeclarations.id)] });
        const refunds = await db.query.taxRefunds.findMany();
        const rmap = Object.fromEntries(refunds.map((r) => [r.declNo, r]));
        const statusText = { declared: "已申报待审", inspecting: "海关查验中", released: "已放行", closed: "已结案" } as const;
        return decls.map((d) => ({
          报关单号: d.declNo, 口岸: d.port, 货物: d.goodsDesc,
          货值: `$${(d.valueUsdCents / 100).toLocaleString()}`,
          状态: statusText[d.status],
          申报日: d.declaredAt.toISOString().slice(0, 10),
          关联退税: rmap[d.declNo] ? `${rmap[d.declNo]!.period} 期 ${yuan(rmap[d.declNo]!.amountCents)}` : "-",
        }));
      },
    }),

    getTaxRefunds: tool({
      description: "查询出口退税进度（申报期/金额/进度百分比/状态）",
      inputSchema: z.object({}),
      execute: async () => {
        const rows = await getDb().query.taxRefunds.findMany({ orderBy: [desc(taxRefunds.id)] });
        const statusText = { filing: "资料申报中", reviewing: "税务审核中", approved: "已核准待退库", paid: "已到账" } as const;
        return rows.map((r) => ({
          所属期: r.period, 退税金额: yuan(r.amountCents),
          进度: `${r.progress}%`, 状态: statusText[r.status],
          关联报关单: r.declNo ?? "-",
        }));
      },
    }),
  };
}
