import { z } from "zod";
import { TRPCError } from "@trpc/server";
import { desc, eq } from "drizzle-orm";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import { fxContracts, fxRates, fxWallets } from "@db/schema";
import { lockCents, spend, unlockCents } from "../lib/bank";

/** 牌价微波动：基于 10 分钟窗口的确定性伪随机（±0.12%），刷新有活感但不乱跳 */
function jitterBps(pair: string, baseBps: number) {
  const slot = Math.floor(Date.now() / (10 * 60 * 1000));
  let h = 0;
  const s = `${pair}:${slot}`;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  const pct = ((h % 2401) / 2401 - 0.5) * 0.0024;
  return Math.round(baseBps * (1 + pct));
}

/** 外币最小单位 × 汇率 → 人民币分 */
export const toCnyCents = (foreignCents: number, rateBps: number) => Math.round((foreignCents * rateBps) / 10000);

export const CURRENCY_META: Record<string, { name: string; symbol: string; flag: string }> = {
  USD: { name: "美元", symbol: "$", flag: "🇺🇸" },
  EUR: { name: "欧元", symbol: "€", flag: "🇪🇺" },
  GBP: { name: "英镑", symbol: "£", flag: "🇬🇧" },
  HKD: { name: "港币", symbol: "HK$", flag: "🇭🇰" },
  JPY: { name: "日元", symbol: "JP¥", flag: "🇯🇵" },
};

const contractInput = z.object({
  currency: z.enum(["USD", "EUR", "GBP", "HKD", "JPY"]),
  foreignAmountCents: z.number().int().positive().max(100000000), // 单笔上限约 $100 万
  tenorDays: z.number().int().min(7).max(365),
  direction: z.enum(["sell_foreign", "buy_foreign"]).default("sell_foreign"),
});

export const fxRouter = createRouter({
  /** 实时牌价（带微波动） */
  rates: publicQuery.query(async () => {
    const rows = await getDb().query.fxRates.findMany();
    return rows.map((r) => ({
      pair: r.pair,
      currency: r.currency,
      name: CURRENCY_META[r.currency]?.name ?? r.currency,
      symbol: CURRENCY_META[r.currency]?.symbol ?? "",
      buyRate: jitterBps(r.pair + "B", r.buyRateBps) / 10000,
      sellRate: jitterBps(r.pair + "S", r.sellRateBps) / 10000,
      updatedAt: new Date(),
    }));
  }),

  /** 多币种钱包 + 人民币等值 */
  wallets: publicQuery.query(async () => {
    const db = getDb();
    const [wallets, rates] = await Promise.all([db.query.fxWallets.findMany(), db.query.fxRates.findMany()]);
    const rateMap = Object.fromEntries(rates.map((r) => [r.currency, jitterBps(r.pair + "B", r.buyRateBps)]));
    return wallets.map((w) => ({
      ...w,
      name: CURRENCY_META[w.currency]?.name ?? w.currency,
      symbol: CURRENCY_META[w.currency]?.symbol ?? "",
      rateBps: rateMap[w.currency] ?? 0,
      cnyCents: toCnyCents(w.balanceForeignCents, rateMap[w.currency] ?? 0),
    }));
  }),

  /** 汇率风险敞口分析 */
  exposure: publicQuery.query(async () => {
    const db = getDb();
    const [wallets, rates, contracts] = await Promise.all([
      db.query.fxWallets.findMany(),
      db.query.fxRates.findMany(),
      db.query.fxContracts.findMany({ where: eq(fxContracts.status, "active") }),
    ]);
    const rateMap = Object.fromEntries(rates.map((r) => [r.currency, jitterBps(r.pair + "B", r.buyRateBps)]));
    const items = wallets
      .filter((w) => w.balanceForeignCents > 0)
      .map((w) => {
        const cnyCents = toCnyCents(w.balanceForeignCents, rateMap[w.currency] ?? 0);
        const hedgedCnyCents = contracts
          .filter((c) => c.currency === w.currency)
          .reduce((s, c) => s + toCnyCents(c.foreignAmountCents, rateMap[c.currency] ?? 0), 0);
        return {
          currency: w.currency,
          name: CURRENCY_META[w.currency]?.name ?? w.currency,
          foreignCents: w.balanceForeignCents,
          cnyCents,
          hedgedCnyCents,
          hedgeRatio: cnyCents > 0 ? Math.min(1, hedgedCnyCents / cnyCents) : 0,
        };
      });
    const totalCnyCents = items.reduce((s, i) => s + i.cnyCents, 0);
    const totalHedgedCnyCents = items.reduce((s, i) => s + i.hedgedCnyCents, 0);
    return {
      items,
      totalCnyCents,
      totalHedgedCnyCents,
      openCnyCents: totalCnyCents - totalHedgedCnyCents,
      hedgeRatio: totalCnyCents > 0 ? totalHedgedCnyCents / totalCnyCents : 0,
    };
  }),

  /** 我的衍生品合约 */
  contracts: publicQuery.query(() => getDb().query.fxContracts.findMany({ orderBy: [desc(fxContracts.id)] })),

  /** 远期锁汇：锁定汇率，冻结 5% 人民币保证金 */
  createForward: publicQuery.input(contractInput).mutation(async ({ input }) => {
    const db = getDb();
    const rateRow = await db.query.fxRates.findFirst({ where: eq(fxRates.currency, input.currency) });
    if (!rateRow) throw new TRPCError({ code: "BAD_REQUEST", message: "暂不支持该币种" });
    const rateBps = input.direction === "sell_foreign" ? rateRow.buyRateBps : rateRow.sellRateBps;

    // 结汇方向校验外币钱包余额
    if (input.direction === "sell_foreign") {
      const wallet = await db.query.fxWallets.findFirst({ where: eq(fxWallets.currency, input.currency) });
      if (!wallet || wallet.balanceForeignCents < input.foreignAmountCents) {
        throw new TRPCError({ code: "BAD_REQUEST", message: `${input.currency} 钱包余额不足，无法锁定该金额的远期结汇` });
      }
    }
    const cnyCents = toCnyCents(input.foreignAmountCents, rateBps);
    const marginCents = Math.round(cnyCents * 0.05);
    await lockCents(marginCents);

    const expiresAt = new Date(Date.now() + input.tenorDays * 86400000);
    const [{ id }] = await db
      .insert(fxContracts)
      .values({
        type: "forward",
        currency: input.currency,
        foreignAmountCents: input.foreignAmountCents,
        lockedRateBps: rateBps,
        direction: input.direction,
        marginCents,
        expiresAt,
      })
      .$returningId();
    return {
      id,
      lockedRate: (rateBps / 10000).toFixed(4),
      marginText: (marginCents / 100).toFixed(2),
      expiresAt,
      message: `远期锁汇成功：${input.currency} 锁定汇率 ${(rateBps / 10000).toFixed(4)}，保证金 ¥${(marginCents / 100).toFixed(2)} 已冻结，${input.tenorDays} 天后到期`,
    };
  }),

  /** 外汇期权：支付 1.5% 期权费，获得到期按约定汇率结汇的权利 */
  createOption: publicQuery.input(contractInput).mutation(async ({ input }) => {
    const db = getDb();
    const rateRow = await db.query.fxRates.findFirst({ where: eq(fxRates.currency, input.currency) });
    if (!rateRow) throw new TRPCError({ code: "BAD_REQUEST", message: "暂不支持该币种" });
    const rateBps = input.direction === "sell_foreign" ? rateRow.buyRateBps : rateRow.sellRateBps;
    const cnyCents = toCnyCents(input.foreignAmountCents, rateBps);
    const premiumCents = Math.round(cnyCents * 0.015);
    await spend({
      amountCents: premiumCents,
      category: "金融衍生品",
      counterparty: "灵犀银行金融市场部",
      memo: `${input.currency} 外汇期权费（名义本金 ${input.currency} ${(input.foreignAmountCents / 100).toLocaleString()}）`,
    });
    const expiresAt = new Date(Date.now() + input.tenorDays * 86400000);
    const [{ id }] = await db
      .insert(fxContracts)
      .values({
        type: "option",
        currency: input.currency,
        foreignAmountCents: input.foreignAmountCents,
        lockedRateBps: rateBps,
        direction: input.direction,
        premiumCents,
        expiresAt,
      })
      .$returningId();
    return {
      id,
      lockedRate: (rateBps / 10000).toFixed(4),
      premiumText: (premiumCents / 100).toFixed(2),
      expiresAt,
      message: `期权合约已生效：支付期权费 ¥${(premiumCents / 100).toFixed(2)}，获得 ${input.tenorDays} 天后按 ${(rateBps / 10000).toFixed(4)} 结汇的权利`,
    };
  }),

  /** 取消合约（仅远期，退回保证金） */
  cancel: publicQuery.input(z.object({ id: z.number().int() })).mutation(async ({ input }) => {
    const db = getDb();
    const c = await db.query.fxContracts.findFirst({ where: eq(fxContracts.id, input.id) });
    if (!c) throw new TRPCError({ code: "NOT_FOUND", message: "合约不存在" });
    if (c.status !== "active") throw new TRPCError({ code: "BAD_REQUEST", message: "该合约已结束，无法取消" });
    if (c.type === "option") throw new TRPCError({ code: "BAD_REQUEST", message: "期权合约一经购买不可取消（期权费不退）" });
    if (c.marginCents > 0) await unlockCents(c.marginCents);
    await db.update(fxContracts).set({ status: "cancelled" }).where(eq(fxContracts.id, c.id));
    return { message: `合约已取消，保证金 ¥${(c.marginCents / 100).toFixed(2)} 已退回活期` };
  }),
});
