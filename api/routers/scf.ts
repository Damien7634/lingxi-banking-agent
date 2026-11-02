import { z } from "zod";
import { TRPCError } from "@trpc/server";
import { desc, eq, inArray } from "drizzle-orm";
import { createRouter, publicQuery } from "../middleware";
import { getDb } from "../queries/connection";
import { scfLoans, scfProducts } from "@db/schema";
import { receive, spend } from "../lib/bank";

export const scfRouter = createRouter({
  /** 供应链金融产品列表 */
  products: publicQuery.query(() => getDb().query.scfProducts.findMany()),

  /** 我的融资（含产品信息） */
  myLoans: publicQuery.query(async () => {
    const db = getDb();
    const loans = await db.query.scfLoans.findMany({ orderBy: [desc(scfLoans.id)] });
    const products = await db.query.scfProducts.findMany({
      where: inArray(scfProducts.id, [...new Set(loans.map((l) => l.productId))]),
    });
    const pmap = Object.fromEntries(products.map((p) => [p.id, p]));
    return loans.map((l) => ({ ...l, product: pmap[l.productId] ?? null }));
  }),

  /** 申请融资：演示环境即时审批放款到活期 */
  applyLoan: publicQuery
    .input(
      z.object({
        productId: z.number().int(),
        amountCents: z.number().int().positive(),
        purpose: z.string().max(100).optional(),
        relatedRef: z.string().max(64).optional(),
      }),
    )
    .mutation(async ({ input }) => {
      const db = getDb();
      const p = await db.query.scfProducts.findFirst({ where: eq(scfProducts.id, input.productId) });
      if (!p) throw new TRPCError({ code: "NOT_FOUND", message: "产品不存在" });
      if (input.amountCents > p.maxAmountCents) {
        throw new TRPCError({
          code: "BAD_REQUEST",
          message: `超出该产品额度上限 ¥${(p.maxAmountCents / 100).toLocaleString("zh-CN")}`,
        });
      }
      // 同一产品在途贷款限 1 笔（风控简化）
      const existing = await db.query.scfLoans.findFirst({
        where: eq(scfLoans.productId, p.id),
      });
      if (existing && existing.status === "disbursed") {
        throw new TRPCError({ code: "BAD_REQUEST", message: `「${p.name}」存在在途融资，请先归还后再申请` });
      }
      const dueAt = new Date(Date.now() + p.termDays * 86400000);
      const [{ id }] = await db
        .insert(scfLoans)
        .values({
          productId: p.id,
          amountCents: input.amountCents,
          rateBps: p.rateBps,
          purpose: input.purpose,
          relatedRef: input.relatedRef,
          status: "disbursed",
          dueAt,
        })
        .$returningId();
      await receive({
        amountCents: input.amountCents,
        category: "融资放款",
        counterparty: "灵犀银行供应链金融",
        memo: `${p.name} 放款（年化 ${(p.rateBps / 100).toFixed(2)}%）`,
      });
      return {
        id,
        product: p.name,
        amountText: (input.amountCents / 100).toFixed(2),
        dueAt,
        message: `「${p.name}」已审批通过并放款 ¥${(input.amountCents / 100).toLocaleString("zh-CN")} 至活期账户，期限 ${p.termDays} 天`,
      };
    }),

  /** 还款（本金+按天计息，从活期扣） */
  repay: publicQuery.input(z.object({ loanId: z.number().int() })).mutation(async ({ input }) => {
    const db = getDb();
    const loan = await db.query.scfLoans.findFirst({ where: eq(scfLoans.id, input.loanId) });
    if (!loan) throw new TRPCError({ code: "NOT_FOUND", message: "融资记录不存在" });
    if (loan.status !== "disbursed") throw new TRPCError({ code: "BAD_REQUEST", message: "该笔融资已结清" });
    const p = await db.query.scfProducts.findFirst({ where: eq(scfProducts.id, loan.productId) });
    const days = Math.max(1, Math.ceil((Date.now() - loan.appliedAt.getTime()) / 86400000));
    const interestCents = Math.round((loan.amountCents * loan.rateBps * days) / 10000 / 365);
    const totalCents = loan.amountCents + interestCents;
    await spend({
      amountCents: totalCents,
      category: "融资还款",
      counterparty: "灵犀银行供应链金融",
      memo: `归还「${p?.name ?? "融资"}」本金+利息（${days} 天）`,
    });
    await db.update(scfLoans).set({ status: "repaid", repaidAt: new Date() }).where(eq(scfLoans.id, loan.id));
    return {
      product: p?.name,
      principalText: (loan.amountCents / 100).toFixed(2),
      interestText: (interestCents / 100).toFixed(2),
      message: `已结清「${p?.name}」：本金 ¥${(loan.amountCents / 100).toLocaleString("zh-CN")} + 利息 ¥${(interestCents / 100).toFixed(2)}`,
    };
  }),
});
