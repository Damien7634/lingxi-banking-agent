import { z } from "zod";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import {
  contacts,
  scheduledTransfers,
  aaSplits,
  transactions,
} from "@db/schema";
import { desc, eq, like, or } from "drizzle-orm";
import { getMainAccount, spend, availableCents, fenToYuan } from "../lib/bank";

/** 按人名 / 手机号 / 备注模糊匹配收款人 */
export async function matchContacts(query: string) {
  const db = getDb();
  const q = `%${query}%`;
  return db.query.contacts.findMany({
    where: or(
      like(contacts.name, q),
      like(contacts.phone, q),
      like(contacts.note, q),
      like(contacts.relation, q),
      like(contacts.cardTail, q),
    ),
  });
}

export const accountRouter = createRouter({
  overview: publicQuery.query(async () => {
    const db = getDb();
    const acc = await getMainAccount();
    const recent = await db.query.transactions.findMany({
      orderBy: [desc(transactions.occurredAt)],
      limit: 8,
    });
    const pendingScheduled = await db.query.scheduledTransfers.findMany({
      where: eq(scheduledTransfers.status, "pending"),
    });
    const scheduled = await Promise.all(
      pendingScheduled.map(async (s) => ({
        ...s,
        contact: await db.query.contacts.findFirst({ where: eq(contacts.id, s.contactId) }),
      })),
    );
    return { account: acc, availableCents: availableCents(acc), recent, scheduled };
  }),
});

export const transferRouter = createRouter({
  searchContacts: publicQuery
    .input(z.object({ query: z.string().min(1) }))
    .query(({ input }) => matchContacts(input.query)),

  listContacts: publicQuery.query(() => getDb().query.contacts.findMany()),

  transfer: publicQuery
    .input(
      z.object({
        contactId: z.number().int().positive(),
        amountCents: z.number().int().positive().max(100_000_000, "单笔转账不能超过 100 万"),
        memo: z.string().max(50).optional(),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const contact = await db.query.contacts.findFirst({ where: eq(contacts.id, input.contactId) });
      if (!contact) throw new Error("收款人不存在");
      const r = await spend({
        amountCents: input.amountCents,
        category: "转账",
        counterparty: contact.name,
        memo: input.memo ?? `转账给${contact.name}`,
      });
      return { ...r, contact, amountText: fenToYuan(input.amountCents) };
    }),

  schedule: publicQuery
    .input(
      z.object({
        contactId: z.number().int().positive(),
        amountCents: z.number().int().positive().max(100_000_000),
        memo: z.string().max(50).optional(),
        executeAt: z.date(),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const contact = await db.query.contacts.findFirst({ where: eq(contacts.id, input.contactId) });
      if (!contact) throw new Error("收款人不存在");
      if (input.executeAt.getTime() < Date.now()) throw new Error("执行时间必须晚于当前时间");
      const [{ id }] = await db
        .insert(scheduledTransfers)
        .values({
          contactId: input.contactId,
          amountCents: input.amountCents,
          memo: input.memo,
          executeAt: input.executeAt,
        })
        .$returningId();
      return { id, contact, executeAt: input.executeAt, amountText: fenToYuan(input.amountCents) };
    }),

  cancelScheduled: publicQuery
    .input(z.object({ id: z.number().int().positive() }))
    .mutation(async ({ input }) => {
      await getDb()
        .update(scheduledTransfers)
        .set({ status: "cancelled" })
        .where(eq(scheduledTransfers.id, input.id));
      return { ok: true };
    }),

  createAASplit: publicQuery
    .input(
      z.object({
        title: z.string().min(1).max(40),
        totalCents: z.number().int().positive().max(10_000_000),
        participants: z
          .array(
            z.object({
              name: z.string().min(1).max(20),
              phone: z.string().max(20).optional(),
            }),
          )
          .min(1)
          .max(20),
      }),
    )
    .mutation(async ({ input }) => {
      const per = Math.floor(input.totalCents / (input.participants.length + 1)); // 不含发起人
      const list = input.participants.map((p, i) => ({
        name: p.name,
        phone: p.phone ?? "",
        amountCents: i === 0 ? input.totalCents - per * input.participants.length : per, // 余数给第一人
        paid: false,
      }));
      const [{ id }] = await getDb()
        .insert(aaSplits)
        .values({ title: input.title, totalCents: input.totalCents, participants: JSON.stringify(list) })
        .$returningId();
      return { id, perPerson: fenToYuan(per), participants: list };
    }),

  listAASplits: publicQuery.query(async () => {
    const rows = await getDb().query.aaSplits.findMany({ orderBy: [desc(aaSplits.createdAt)] });
    return rows.map((r) => ({ ...r, participants: JSON.parse(r.participants) as Array<{ name: string; phone: string; amountCents: number; paid: boolean }> }));
  }),

  markAAPaid: publicQuery
    .input(z.object({ id: z.number().int().positive(), name: z.string().min(1) }))
    .mutation(async ({ input }) => {
      const db = getDb();
      const row = await db.query.aaSplits.findFirst({ where: eq(aaSplits.id, input.id) });
      if (!row) throw new Error("AA 收款不存在");
      const list = JSON.parse(row.participants) as Array<{ name: string; phone: string; amountCents: number; paid: boolean }>;
      const target = list.find((p) => p.name === input.name);
      if (!target) throw new Error(`参与者中没有「${input.name}」`);
      target.paid = true;
      const done = list.every((p) => p.paid);
      await db
        .update(aaSplits)
        .set({ participants: JSON.stringify(list), status: done ? "done" : "collecting" })
        .where(eq(aaSplits.id, input.id));
      return { ok: true, done };
    }),
});
