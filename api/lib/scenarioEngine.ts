/**
 * 跨场景任务引擎：生日守护
 *
 * 状态机：armed → locked（进入生日当月，锁定活期）→ ordered（生日前 N 天，下单鲜花/蛋糕）→ done（生日当天，解锁余款）
 * 每次读取概览 / Agent 会话前执行一次 tick，让任务随时间自然推进。
 */
import { getDb } from "../queries/connection";
import { scenarios, giftOrders, contacts } from "@db/schema";
import { eq } from "drizzle-orm";
import { lockCents, unlockCents, fenToYuan } from "./bank";

export type GiftPlan = {
  kind: "flower" | "cake";
  merchant: string;
  itemName: string;
  amountCents: number;
};

export type BirthdayPayload = {
  lockCents: number;
  birthday: string; // MM-DD
  orderLeadDays: number; // 生日前几天下单
  gifts: GiftPlan[];
};

type ScenarioRow = typeof scenarios.$inferSelect;

function parseLog(row: ScenarioRow): Array<{ at: string; text: string }> {
  try {
    return JSON.parse(row.log);
  } catch {
    return [];
  }
}

export function parsePayload(row: ScenarioRow): BirthdayPayload {
  return JSON.parse(row.payload) as BirthdayPayload;
}

async function appendLog(row: ScenarioRow, text: string) {
  const log = parseLog(row);
  log.push({ at: new Date().toISOString(), text });
  await getDb().update(scenarios).set({ log: JSON.stringify(log), updatedAt: new Date() }).where(eq(scenarios.id, row.id));
}

/** 推进所有进行中的生日守护任务。now 可注入以便演示“时间快进”。 */
export async function tickBirthdayGuard(now = new Date()) {
  const db = getDb();
  const rows = await db.query.scenarios.findMany({ where: eq(scenarios.type, "birthday_guard") });
  const events: string[] = [];

  for (const row of rows) {
    if (row.status === "done" || row.status === "cancelled") continue;
    const p = parsePayload(row);
    const [mm, dd] = p.birthday.split("-").map(Number);
    const year = now.getFullYear();
    // 若今年生日已过且任务仍在早期状态，顺延到下一年
    let birthday = new Date(year, mm - 1, dd, 12, 0, 0);
    if (birthday.getTime() < now.getTime() - 24 * 3600 * 1000 && row.status === "armed") {
      birthday = new Date(year + 1, mm - 1, dd, 12, 0, 0);
    }
    const contact = await db.query.contacts.findFirst({ where: eq(contacts.id, row.contactId) });
    const cname = contact?.name ?? "对方";
    const isBirthdayMonth = birthday.getMonth() === now.getMonth();
    const msToBirthday = birthday.getTime() - now.getTime();
    const daysToBirthday = Math.ceil(msToBirthday / (24 * 3600 * 1000));

    // 1) 进入生日当月：锁定活期
    if (row.status === "armed" && isBirthdayMonth) {
      await lockCents(p.lockCents);
      await db.update(scenarios).set({ status: "locked", updatedAt: now }).where(eq(scenarios.id, row.id));
      const msg = `已进入${cname}生日月，锁定活期 ¥${fenToYuan(p.lockCents)}，专款专用`;
      await appendLog({ ...row, status: "locked" }, msg);
      events.push(msg);
      row.status = "locked";
    }

    // 2) 生日前 N 天：下单鲜花/蛋糕
    if (row.status === "locked" && daysToBirthday <= p.orderLeadDays && daysToBirthday >= 0) {
      for (const g of p.gifts) {
        await db.insert(giftOrders).values({
          scenarioId: row.id,
          kind: g.kind,
          merchant: g.merchant,
          itemName: g.itemName,
          amountCents: g.amountCents,
          deliverAt: birthday,
          status: "placed",
        });
      }
      await db.update(scenarios).set({ status: "ordered", updatedAt: now }).where(eq(scenarios.id, row.id));
      const total = p.gifts.reduce((s, g) => s + g.amountCents, 0);
      const names = p.gifts.map((g) => `「${g.itemName}」`).join("和");
      const msg = `已下单 ${names}，合计 ¥${fenToYuan(total)}，将于 ${p.birthday} 送达`;
      await appendLog({ ...row, status: "ordered" }, msg);
      events.push(msg);
      row.status = "ordered";
    }

    // 3) 生日当天及之后：解锁余款，任务完成
    if ((row.status === "ordered" || row.status === "locked") && daysToBirthday <= 0) {
      await unlockCents(p.lockCents);
      await db
        .update(giftOrders)
        .set({ status: "delivered" })
        .where(eq(giftOrders.scenarioId, row.id));
      await db.update(scenarios).set({ status: "done", updatedAt: now }).where(eq(scenarios.id, row.id));
      const msg = `今天是${cname}生日，礼品已送达，锁定的 ¥${fenToYuan(p.lockCents)} 已解锁`;
      await appendLog({ ...row, status: "done" }, msg);
      events.push(msg);
      row.status = "done";
    }
  }
  return events;
}

export const DEFAULT_GIFTS: GiftPlan[] = [
  { kind: "flower", merchant: "花加", itemName: "向日葵混搭花束 · 19 枝", amountCents: 26800 },
  { kind: "cake", merchant: "诺心", itemName: "6 寸草莓奶油生日蛋糕", amountCents: 19800 },
];
