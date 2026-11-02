import { z } from "zod";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import { transactions } from "@db/schema";
import { and, desc, eq, gte, lt } from "drizzle-orm";

export const CATEGORIES = ["餐饮", "购物", "交通", "住房", "医疗", "娱乐", "教育", "转账", "理财", "工资", "其他"] as const;

function monthRange(year: number, month: number) {
  // month: 1-12
  const start = new Date(year, month - 1, 1);
  const end = new Date(year, month, 1);
  return { start, end };
}

async function txInRange(start: Date, end: Date) {
  return getDb().query.transactions.findMany({
    where: and(gte(transactions.occurredAt, start), lt(transactions.occurredAt, end)),
    orderBy: [desc(transactions.occurredAt)],
  });
}

function summarize(rows: Array<typeof transactions.$inferSelect>) {
  let totalIn = 0;
  let totalOut = 0;
  const byCategory = new Map<string, number>();
  const byCounterparty = new Map<string, number>();
  for (const t of rows) {
    if (t.direction === "in") {
      totalIn += t.amountCents;
      continue;
    }
    totalOut += t.amountCents;
    byCategory.set(t.category, (byCategory.get(t.category) ?? 0) + t.amountCents);
    byCounterparty.set(t.counterparty, (byCounterparty.get(t.counterparty) ?? 0) + t.amountCents);
  }
  const topCategories = [...byCategory.entries()]
    .map(([category, cents]) => ({ category, cents, pct: totalOut ? Math.round((cents / totalOut) * 1000) / 10 : 0 }))
    .sort((a, b) => b.cents - a.cents);
  const topCounterparties = [...byCounterparty.entries()]
    .map(([name, cents]) => ({ name, cents }))
    .sort((a, b) => b.cents - a.cents)
    .slice(0, 5);
  return { totalInCents: totalIn, totalOutCents: totalOut, topCategories, topCounterparties, count: rows.length };
}

/** 规则引擎：识别可疑交易（夜间大额 / 陌生对手方大额 / 高频同类支出） */
export async function detectAnomalies(days = 90) {
  const since = new Date(Date.now() - days * 24 * 3600 * 1000);
  const rows = await getDb().query.transactions.findMany({
    where: and(gte(transactions.occurredAt, since), eq(transactions.direction, "out")),
    orderBy: [desc(transactions.occurredAt)],
  });

  // 历史对手方集合（90 天前有过往来的算“熟人”）
  const all = await getDb().query.transactions.findMany();
  const knownCp = new Set(
    all.filter((t) => t.occurredAt < since && t.direction === "out").map((t) => t.counterparty),
  );

  const flagged: Array<{ tx: typeof transactions.$inferSelect; reasons: string[] }> = [];
  const byDayCat = new Map<string, number>();

  for (const t of rows) {
    const reasons: string[] = [];
    const hour = t.occurredAt.getHours();
    if (hour >= 0 && hour < 5 && t.amountCents >= 100_000) {
      reasons.push(`凌晨 ${hour} 点发生大额支出（¥${(t.amountCents / 100).toFixed(2)}）`);
    }
    if (t.category === "转账" && !knownCp.has(t.counterparty) && t.amountCents >= 500_000) {
      reasons.push(`收款方「${t.counterparty}」无历史往来记录，单笔金额较大`);
    }
    const key = `${t.occurredAt.toDateString()}|${t.category}`;
    byDayCat.set(key, (byDayCat.get(key) ?? 0) + t.amountCents);
    if (byDayCat.get(key)! >= 300_000 && ["娱乐", "游戏"].includes(t.category)) {
      reasons.push(`当日「${t.category}」类支出累计 ¥${(byDayCat.get(key)! / 100).toFixed(2)}，频次/金额异常`);
    }
    if (t.isAnomaly && t.anomalyReason) reasons.push(t.anomalyReason);
    if (reasons.length > 0) flagged.push({ tx: t, reasons: [...new Set(reasons)] });
  }
  // 按 id 去重
  const seen = new Set<number>();
  return flagged.filter((f) => (seen.has(f.tx.id) ? false : (seen.add(f.tx.id), true)));
}

export const billRouter = createRouter({
  monthly: publicQuery
    .input(z.object({ year: z.number().int(), month: z.number().int().min(1).max(12) }))
    .query(async ({ input }) => {
      const { start, end } = monthRange(input.year, input.month);
      const prev = monthRange(input.month === 1 ? input.year - 1 : input.year, input.month === 1 ? 12 : input.month - 1);
      const [rows, prevRows] = await Promise.all([txInRange(start, end), txInRange(prev.start, prev.end)]);
      const cur = summarize(rows);
      const prevSum = summarize(prevRows);
      const momPct =
        prevSum.totalOutCents > 0
          ? Math.round(((cur.totalOutCents - prevSum.totalOutCents) / prevSum.totalOutCents) * 1000) / 10
          : null;
      return {
        year: input.year,
        month: input.month,
        ...cur,
        prevTotalOutCents: prevSum.totalOutCents,
        momPct,
        rows: rows.slice(0, 200),
      };
    }),

  yearly: publicQuery
    .input(z.object({ year: z.number().int() }))
    .query(async ({ input }) => {
      const rows = await txInRange(new Date(input.year, 0, 1), new Date(input.year + 1, 0, 1));
      const months = Array.from({ length: 12 }, (_, i) => {
        const m = rows.filter((t) => t.occurredAt.getMonth() === i);
        const s = summarize(m);
        return { month: i + 1, inCents: s.totalInCents, outCents: s.totalOutCents };
      });
      const total = summarize(rows);
      return { year: input.year, months, ...total };
    }),

  anomalies: publicQuery.input(z.object({ days: z.number().int().min(7).max(365).default(90) })).query(({ input }) =>
    detectAnomalies(input.days),
  ),
});
