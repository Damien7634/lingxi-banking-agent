import { z } from "zod";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import { cards, scenarios, giftOrders, contacts } from "@db/schema";
import { and, desc, eq, ne } from "drizzle-orm";
import { unlockCents } from "../lib/bank";
import { parsePayload, DEFAULT_GIFTS, tickBirthdayGuard } from "../lib/scenarioEngine";
import { matchContacts } from "./account";

export const cardRouter = createRouter({
  list: publicQuery.query(() => getDb().query.cards.findMany({ orderBy: [desc(cards.createdAt)] })),

  applyCard: publicQuery
    .input(
      z.object({
        cardType: z.enum(["debit", "credit"]),
        holderName: z.string().min(1).max(30).optional(),
      }),
    )
    .mutation(async ({ input }) => {
      const last4 = String(Math.floor(1000 + Math.random() * 9000));
      const [{ id }] = await getDb()
        .insert(cards)
        .values({
          cardType: input.cardType,
          bankName: "灵犀银行",
          last4,
          creditLimitCents: input.cardType === "credit" ? 3000000 : null,
          status: "normal",
        })
        .$returningId();
      return { id, last4, cardType: input.cardType, message: input.cardType === "credit" ? "信用卡申请已通过预审，初始额度 ¥30,000" : "借记卡开立成功" };
    }),

  adjustLimit: publicQuery
    .input(
      z.object({
        cardId: z.number().int().positive(),
        creditLimitCents: z.number().int().positive().max(2000_0000).optional(),
        dailyLimitCents: z.number().int().positive().max(5000_0000).optional(),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const card = await db.query.cards.findFirst({ where: eq(cards.id, input.cardId) });
      if (!card) throw new Error("卡片不存在");
      if (card.status === "lost") throw new Error("卡片已挂失，不能调整额度");
      const set: Partial<typeof cards.$inferInsert> = {};
      if (input.creditLimitCents && card.cardType === "credit") set.creditLimitCents = input.creditLimitCents;
      if (input.dailyLimitCents) set.dailyLimitCents = input.dailyLimitCents;
      if (Object.keys(set).length === 0) throw new Error("没有可调整的额度项");
      await db.update(cards).set(set).where(eq(cards.id, input.cardId));
      return { ok: true, cardId: input.cardId, ...set };
    }),

  setStatus: publicQuery
    .input(
      z.object({
        cardId: z.number().int().positive(),
        action: z.enum(["freeze", "unfreeze", "reportLost", "restrict", "unrestrict"]),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const card = await db.query.cards.findFirst({ where: eq(cards.id, input.cardId) });
      if (!card) throw new Error("卡片不存在");
      const map: Record<typeof input.action, typeof cards.$inferSelect.status> = {
        freeze: "frozen",
        unfreeze: "normal",
        reportLost: "lost",
        restrict: "restricted",
        unrestrict: "normal",
      };
      if (card.status === "lost" && input.action !== "unfreeze") {
        throw new Error("卡片已挂失，请先补办新卡");
      }
      await db.update(cards).set({ status: map[input.action] }).where(eq(cards.id, input.cardId));
      const label = { freeze: "临时冻结", unfreeze: "解除冻结", reportLost: "挂失", restrict: "限制交易", unrestrict: "解除交易限制" }[input.action];
      return { ok: true, cardId: input.cardId, status: map[input.action], message: `尾号 ${card.last4} 已${label}` };
    }),

  toggleOnlinePay: publicQuery
    .input(z.object({ cardId: z.number().int().positive(), enabled: z.boolean() }))
    .mutation(async ({ input }) => {
      await getDb().update(cards).set({ onlinePayEnabled: input.enabled }).where(eq(cards.id, input.cardId));
      return { ok: true };
    }),
});

export const scenarioRouter = createRouter({
  /** 创建生日守护：检测联系人生日 → 当月锁定活期 → 前 N 天订鲜花蛋糕 */
  createBirthdayGuard: publicQuery
    .input(
      z.object({
        contactQuery: z.string().min(1), // 人名/手机号/备注
        lockCents: z.number().int().positive().max(1_000_000).default(100000),
        orderLeadDays: z.number().int().min(1).max(7).default(2),
        giftBudgetCents: z.number().int().positive().max(200000).optional(),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const matched = await matchContacts(input.contactQuery);
      if (matched.length === 0) throw new Error(`没有找到与「${input.contactQuery}」相关的联系人`);
      const contact = matched[0];
      if (!contact.birthday) throw new Error(`联系人「${contact.name}」没有记录生日，请先在通讯录补充`);
      // 同一个人不重复布防
      const existing = await db.query.scenarios.findMany({
        where: eq(scenarios.contactId, contact.id),
      });
      const active = existing.find((s) => !["done", "cancelled"].includes(s.status));
      if (active) throw new Error(`「${contact.name}」已有进行中的守护任务（${active.title}）`);

      const payload = {
        lockCents: input.lockCents,
        birthday: contact.birthday,
        orderLeadDays: input.orderLeadDays,
        gifts: DEFAULT_GIFTS,
      };
      const [{ id }] = await db
        .insert(scenarios)
        .values({
          type: "birthday_guard",
          title: `${contact.name}生日守护`,
          contactId: contact.id,
          payload: JSON.stringify(payload),
          status: "armed",
          log: JSON.stringify([
            {
              at: new Date().toISOString(),
              text: `守护已开启：监测到${contact.name}生日为 ${contact.birthday}，当月将锁定 ¥${(input.lockCents / 100).toFixed(2)} 活期，生日前 ${input.orderLeadDays} 天自动下单鲜花和蛋糕`,
            },
          ]),
        })
        .$returningId();
      // 立即推进一次（如果已经是生日当月，马上锁定）
      const events = await tickBirthdayGuard();
      return { id, contact, payload, events };
    }),

  list: publicQuery.query(async () => {
    await tickBirthdayGuard();
    const db = getDb();
    const rows = await db.query.scenarios.findMany({ orderBy: [desc(scenarios.createdAt)] });
    return Promise.all(
      rows.map(async (r) => ({
        ...r,
        payload: parsePayload(r),
        log: JSON.parse(r.log) as Array<{ at: string; text: string }>,
        contact: await db.query.contacts.findFirst({ where: eq(contacts.id, r.contactId) }),
        orders: await db.query.giftOrders.findMany({ where: eq(giftOrders.scenarioId, r.id) }),
      })),
    );
  }),

  cancel: publicQuery
    .input(z.object({ id: z.number().int().positive() }))
    .mutation(async ({ input }) => {
      const db = getDb();
      const row = await db.query.scenarios.findFirst({ where: eq(scenarios.id, input.id) });
      if (!row) throw new Error("任务不存在");
      if (["done", "cancelled"].includes(row.status)) return { ok: true };
      // 取消时解锁资金、取消未送达订单
      if (row.status === "locked" || row.status === "ordered") {
        await unlockCents(parsePayload(row).lockCents);
      }
      await db
        .update(giftOrders)
        .set({ status: "cancelled" })
        .where(and(eq(giftOrders.scenarioId, row.id), ne(giftOrders.status, "delivered")));
      await db.update(scenarios).set({ status: "cancelled", updatedAt: new Date() }).where(eq(scenarios.id, row.id));
      return { ok: true };
    }),
});
