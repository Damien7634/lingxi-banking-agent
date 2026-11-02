import { getDb } from "../queries/connection";
import { accounts, transactions } from "@db/schema";
import { asc, eq } from "drizzle-orm";

export async function getMainAccount() {
  // 单账户演示：取最早创建的主账户（不硬编码 id，重跑 seed 后自增 id 会变）
  const acc = await getDb().query.accounts.findFirst({ orderBy: [asc(accounts.id)] });
  if (!acc) throw new Error("主账户不存在，请先运行 seed");
  return acc;
}

/** 可用余额 = 总余额 - 场景锁定 */
export function availableCents(acc: { balanceCents: number; lockedCents: number }) {
  return acc.balanceCents - acc.lockedCents;
}

/** 支出并记一笔流水（无事务嵌套需求，单账户演示足够） */
export async function spend(opts: {
  amountCents: number;
  category: string;
  counterparty: string;
  memo?: string;
  channel?: string;
}) {
  const db = getDb();
  const acc = await getMainAccount();
  if (availableCents(acc) < opts.amountCents) {
    throw new Error(
      `可用余额不足：当前可用 ¥${(availableCents(acc) / 100).toFixed(2)}，需支付 ¥${(opts.amountCents / 100).toFixed(2)}（另有 ¥${(acc.lockedCents / 100).toFixed(2)} 被场景任务锁定）`,
    );
  }
  await db
    .update(accounts)
    .set({ balanceCents: acc.balanceCents - opts.amountCents, updatedAt: new Date() })
    .where(eq(accounts.id, acc.id));
  const [{ id }] = await db
    .insert(transactions)
    .values({
      direction: "out",
      amountCents: opts.amountCents,
      category: opts.category,
      counterparty: opts.counterparty,
      memo: opts.memo,
      channel: opts.channel ?? "手机银行",
      occurredAt: new Date(),
    })
    .$returningId();
  return { txId: id, balanceCents: acc.balanceCents - opts.amountCents };
}

/** 收入并记一笔流水 */
export async function receive(opts: {
  amountCents: number;
  category: string;
  counterparty: string;
  memo?: string;
  channel?: string;
}) {
  const db = getDb();
  const acc = await getMainAccount();
  await db
    .update(accounts)
    .set({ balanceCents: acc.balanceCents + opts.amountCents, updatedAt: new Date() })
    .where(eq(accounts.id, acc.id));
  const [{ id }] = await db
    .insert(transactions)
    .values({
      direction: "in",
      amountCents: opts.amountCents,
      category: opts.category,
      counterparty: opts.counterparty,
      memo: opts.memo,
      channel: opts.channel ?? "手机银行",
      occurredAt: new Date(),
    })
    .$returningId();
  return { txId: id, balanceCents: acc.balanceCents + opts.amountCents };
}

/** 锁定/解锁活期 */
export async function lockCents(amountCents: number) {
  const db = getDb();
  const acc = await getMainAccount();
  if (availableCents(acc) < amountCents) {
    throw new Error(
      `可锁定余额不足：可用 ¥${(availableCents(acc) / 100).toFixed(2)}，需锁定 ¥${(amountCents / 100).toFixed(2)}`,
    );
  }
  await db
    .update(accounts)
    .set({ lockedCents: acc.lockedCents + amountCents, updatedAt: new Date() })
    .where(eq(accounts.id, acc.id));
}

export async function unlockCents(amountCents: number) {
  const db = getDb();
  const acc = await getMainAccount();
  const next = Math.max(0, acc.lockedCents - amountCents);
  await db.update(accounts).set({ lockedCents: next, updatedAt: new Date() }).where(eq(accounts.id, acc.id));
}

export const fenToYuan = (cents: number) => (cents / 100).toFixed(2);
