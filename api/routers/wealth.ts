import { z } from "zod";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import { wealthProducts, holdings, riskProfiles } from "@db/schema";
import { and, desc, eq } from "drizzle-orm";
import { getMainAccount, spend, receive, fenToYuan } from "../lib/bank";

const RISK_ORDER = ["R1", "R2", "R3", "R4", "R5"] as const;
const LEVEL_TO_MAX_RISK: Record<string, (typeof RISK_ORDER)[number]> = {
  保守型: "R1",
  稳健型: "R2",
  平衡型: "R3",
  进取型: "R4",
  激进型: "R5",
};

function scoreToLevel(score: number) {
  if (score <= 30) return "保守型";
  if (score <= 50) return "稳健型";
  if (score <= 70) return "平衡型";
  if (score <= 85) return "进取型";
  return "激进型";
}

async function latestRiskProfile() {
  const rows = await getDb().query.riskProfiles.findMany({ orderBy: [desc(riskProfiles.createdAt)], limit: 1 });
  return rows[0] ?? null;
}

/** 按用户风险等级过滤并排序推荐 */
export async function recommendProducts(limit = 3) {
  const db = getDb();
  const profile = await latestRiskProfile();
  const maxRisk = profile ? LEVEL_TO_MAX_RISK[profile.level] : "R2"; // 未测评按稳健型保守推荐
  const maxIdx = RISK_ORDER.indexOf(maxRisk);
  const all = await db.query.wealthProducts.findMany();
  const eligible = all.filter((p) => RISK_ORDER.indexOf(p.riskLevel) <= maxIdx);
  eligible.sort((a, b) => b.annualRateBps - a.annualRateBps);
  return { profile, maxRisk: maxRisk, products: eligible.slice(0, limit) };
}

export const wealthRouter = createRouter({
  products: publicQuery.query(() => getDb().query.wealthProducts.findMany()),

  recommend: publicQuery.query(() => recommendProducts(4)),

  riskProfile: publicQuery.query(() => latestRiskProfile()),

  submitRiskAssessment: publicQuery
    .input(
      z.object({
        score: z.number().int().min(0).max(100),
        answers: z.record(z.string(), z.string()),
      }),
    )
    .mutation(async ({ input }) => {
      const level = scoreToLevel(input.score);
      const [{ id }] = await getDb()
        .insert(riskProfiles)
        .values({ score: input.score, level, answers: JSON.stringify(input.answers) })
        .$returningId();
      return { id, score: input.score, level };
    }),

  holdings: publicQuery.query(async () => {
    const db = getDb();
    const rows = await db.query.holdings.findMany({
      where: eq(holdings.status, "holding"),
      orderBy: [desc(holdings.purchasedAt)],
    });
    return Promise.all(
      rows.map(async (h) => ({
        ...h,
        product: await db.query.wealthProducts.findFirst({ where: eq(wealthProducts.id, h.productId) }),
      })),
    );
  }),

  purchase: publicQuery
    .input(
      z.object({
        productId: z.number().int().positive(),
        amountCents: z.number().int().positive().max(100_000_000),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const product = await db.query.wealthProducts.findFirst({ where: eq(wealthProducts.id, input.productId) });
      if (!product) throw new Error("产品不存在");

      // 适当性校验：风险等级不得超过测评结果
      const profile = await latestRiskProfile();
      const maxRisk = LEVEL_TO_MAX_RISK[profile?.level ?? "稳健型"];
      if (RISK_ORDER.indexOf(product.riskLevel) > RISK_ORDER.indexOf(maxRisk)) {
        throw new Error(
          `适当性拦截：您的风险测评结果为「${profile?.level ?? "稳健型"}」，该产品为 ${product.riskLevel}，超出您的风险承受能力`,
        );
      }
      if (input.amountCents < product.minAmountCents) {
        throw new Error(`该产品起购金额为 ¥${fenToYuan(product.minAmountCents)}`);
      }
      await getMainAccount();
      const r = await spend({
        amountCents: input.amountCents,
        category: "理财",
        counterparty: "灵犀理财",
        memo: `申购 ${product.name}`,
      });
      // 同产品持仓合并
      const existing = await db.query.holdings.findFirst({
        where: and(eq(holdings.productId, product.id), eq(holdings.status, "holding")),
      });
      if (existing) {
        await db
          .update(holdings)
          .set({ amountCents: existing.amountCents + input.amountCents })
          .where(eq(holdings.id, existing.id));
        return { holdingId: existing.id, merged: true, product: product.name, balanceCents: r.balanceCents };
      }
      const [{ id }] = await db
        .insert(holdings)
        .values({ productId: product.id, amountCents: input.amountCents })
        .$returningId();
      return { holdingId: id, merged: false, product: product.name, balanceCents: r.balanceCents };
    }),

  redeem: publicQuery
    .input(
      z.object({
        holdingId: z.number().int().positive(),
        amountCents: z.number().int().positive().optional(), // 缺省 = 全额赎回
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const h = await db.query.holdings.findFirst({
        where: and(eq(holdings.id, input.holdingId), eq(holdings.status, "holding")),
      });
      if (!h) throw new Error("持仓不存在或已赎回");
      const product = await db.query.wealthProducts.findFirst({ where: eq(wealthProducts.id, h.productId) });
      const amount = Math.min(input.amountCents ?? h.amountCents, h.amountCents);
      const full = amount >= h.amountCents;
      await receive({
        amountCents: amount + (full ? h.profitCents : 0),
        category: "理财",
        counterparty: "灵犀理财",
        memo: `赎回 ${product?.name ?? "理财"}`,
      });
      await db
        .update(holdings)
        .set(
          full
            ? { status: "redeemed", redeemedAt: new Date(), amountCents: 0 }
            : { amountCents: h.amountCents - amount },
        )
        .where(eq(holdings.id, h.id));
      return {
        ok: true,
        redeemedCents: amount,
        profitCents: full ? h.profitCents : 0,
        product: product?.name,
      };
    }),
});
