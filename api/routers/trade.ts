import { z } from "zod";
import { TRPCError } from "@trpc/server";
import { desc, inArray } from "drizzle-orm";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import { customsDeclarations, shipments, taxRefunds } from "@db/schema";

type Milestone = { time: string; text: string; location: string };

function parseMilestones(raw: string): Milestone[] {
  try {
    const arr = JSON.parse(raw);
    return Array.isArray(arr) ? arr : [];
  } catch {
    return [];
  }
}

export const tradeRouter = createRouter({
  /** 外贸驾驶舱汇总 */
  overview: publicQuery.query(async () => {
    const db = getDb();
    const [ships, decls, refunds] = await Promise.all([
      db.query.shipments.findMany(),
      db.query.customsDeclarations.findMany(),
      db.query.taxRefunds.findMany(),
    ]);
    return {
      activeShipments: ships.filter((s) => !["delivered"].includes(s.status)).length,
      pendingCustoms: decls.filter((d) => ["declared", "inspecting"].includes(d.status)).length,
      refundInProgressCents: refunds.filter((r) => ["filing", "reviewing", "approved"].includes(r.status)).reduce((s, r) => s + r.amountCents, 0),
    };
  }),

  /** 运单列表（轨迹解析好） */
  shipments: publicQuery.query(async () => {
    const rows = await getDb().query.shipments.findMany({ orderBy: [desc(shipments.id)] });
    return rows.map((s) => ({ ...s, milestones: parseMilestones(s.milestones) }));
  }),

  /** 单号查询（支持模糊） */
  track: publicQuery.input(z.object({ trackingNo: z.string().min(4) })).query(async ({ input }) => {
    const rows = await getDb().query.shipments.findMany({ orderBy: [desc(shipments.id)] });
    const kw = input.trackingNo.trim().toUpperCase();
    const hit = rows.find((s) => s.trackingNo.toUpperCase() === kw) ?? rows.find((s) => s.trackingNo.toUpperCase().includes(kw));
    if (!hit) throw new TRPCError({ code: "NOT_FOUND", message: `未找到运单 ${input.trackingNo}，请核对单号` });
    return { ...hit, milestones: parseMilestones(hit.milestones) };
  }),

  /** 报关单列表 */
  customs: publicQuery.query(async () => {
    const db = getDb();
    const decls = await db.query.customsDeclarations.findMany({ orderBy: [desc(customsDeclarations.id)] });
    const refundRows = await db.query.taxRefunds.findMany({ where: inArray(taxRefunds.declNo, decls.map((d) => d.declNo)) });
    const rmap = Object.fromEntries(refundRows.map((r) => [r.declNo, r]));
    return decls.map((d) => ({ ...d, refund: rmap[d.declNo] ?? null }));
  }),

  /** 出口退税进度 */
  taxRefunds: publicQuery.query(() => getDb().query.taxRefunds.findMany({ orderBy: [desc(taxRefunds.id)] })),
});
