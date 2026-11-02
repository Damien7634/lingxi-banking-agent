import { useState } from "react";
import { trpc } from "@/providers/trpc";
import { fmtMoney, RISK_COLOR } from "@/lib/format";
import { PageHeader } from "@/components/common";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { toast } from "sonner";
import { GitCompareArrows, Loader2, Sparkles } from "lucide-react";

const TYPE_LABEL: Record<string, string> = { cash: "现金管理", bond: "固收", mixed: "混合", index: "指数", gold: "黄金" };

type Props = { openAgent: (prompt?: string) => void };

export default function WealthPage({ openAgent }: Props) {
  const utils = trpc.useUtils();
  const products = trpc.wealth.products.useQuery();
  const holdingsQ = trpc.wealth.holdings.useQuery();
  const profile = trpc.wealth.riskProfile.useQuery();
  const [compareIds, setCompareIds] = useState<number[]>([]);
  const [compareOpen, setCompareOpen] = useState(false);
  const [buyId, setBuyId] = useState<number | null>(null);
  const [redeemId, setRedeemId] = useState<number | null>(null);
  const [assessOpen, setAssessOpen] = useState(false);

  const toggleCompare = (id: number) => {
    setCompareIds((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : prev.length >= 3 ? (toast.error("最多对比 3 只产品"), prev) : [...prev, id]));
  };

  const totalHolding = (holdingsQ.data ?? []).reduce((s, h) => s + h.amountCents, 0);
  const totalProfit = (holdingsQ.data ?? []).reduce((s, h) => s + h.profitCents, 0);

  return (
    <div>
      <PageHeader title="理财" sub="产品推荐 · 对比 · 申购赎回" />

      {/* 持仓概览 */}
      <section className="mx-5 rounded-2xl bg-primary p-5 text-primary-foreground">
        <div className="text-xs opacity-80">持仓总金额（元）</div>
        <div className="num mt-1 text-3xl font-bold">{holdingsQ.data ? fmtMoney(totalHolding).slice(1) : "…"}</div>
        <div className="mt-2 text-xs opacity-90">
          累计收益 <span className="num font-semibold">{fmtMoney(totalProfit)}</span>
        </div>
        <div className="mt-3 flex items-center justify-between">
          <span className="rounded-full bg-white/15 px-2.5 py-1 text-[11px]">
            风险测评：{profile.data ? `${profile.data.level}（${profile.data.score} 分）` : "未测评"}
          </span>
          <button
            onClick={() => setAssessOpen(true)}
            className="rounded-full bg-white/15 px-3 py-1.5 text-[11px] font-medium transition hover:bg-white/25 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
          >
            {profile.data ? "重新测评" : "立即测评"}
          </button>
        </div>
      </section>

      {/* 我的持仓 */}
      {(holdingsQ.data?.length ?? 0) > 0 && (
        <section className="mx-5 mt-4 rounded-2xl bg-card p-4 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
          <h2 className="px-1 text-sm font-semibold">我的持仓</h2>
          <div className="mt-1 divide-y">
            {holdingsQ.data!.map((h) => (
              <div key={h.id} className="flex items-center justify-between py-3">
                <div>
                  <div className="text-sm font-medium">{h.product?.name}</div>
                  <div className="num mt-0.5 text-xs text-muted-foreground">
                    持有 {fmtMoney(h.amountCents)} · 收益 <span className="text-emerald-600">{fmtMoney(h.profitCents)}</span>
                  </div>
                </div>
                <Button variant="outline" size="sm" className="h-9" onClick={() => setRedeemId(h.id)}>
                  赎回
                </Button>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* 产品列表 */}
      <section className="mx-5 mt-4">
        <div className="flex items-center justify-between px-1">
          <h2 className="text-sm font-semibold">在售产品</h2>
          <button
            onClick={() => (compareIds.length >= 2 ? setCompareOpen(true) : toast.info("勾选 2-3 只产品后可对比"))}
            className="flex h-9 items-center gap-1 rounded-full bg-secondary px-3 text-xs font-medium transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <GitCompareArrows className="h-3.5 w-3.5" />
            对比（{compareIds.length}）
          </button>
        </div>
        <div className="mt-3 space-y-3">
          {products.data?.map((p) => (
            <div key={p.id} className="rounded-2xl bg-card p-4 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-semibold">{p.name}</span>
                    <span className={`shrink-0 rounded px-1.5 py-0.5 text-[10px] font-bold ${RISK_COLOR[p.riskLevel]}`}>{p.riskLevel}</span>
                  </div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {TYPE_LABEL[p.type]} · {p.termDays === 0 ? "灵活申赎" : `${p.termDays} 天`} · 起购 {fmtMoney(p.minAmountCents)}
                  </div>
                </div>
                <div className="shrink-0 text-right">
                  <div className="num text-lg font-bold text-primary">{p.annualRateBps > 0 ? `${(p.annualRateBps / 100).toFixed(2)}%` : "随市价"}</div>
                  <div className="text-[10px] text-muted-foreground">业绩比较基准</div>
                </div>
              </div>
              <p className="mt-2 text-xs leading-relaxed text-muted-foreground">{p.description}</p>
              <div className="mt-3 flex gap-2">
                <Button size="sm" className="h-10 flex-1" onClick={() => setBuyId(p.id)}>
                  申购
                </Button>
                <Button
                  size="sm"
                  variant={compareIds.includes(p.id) ? "default" : "outline"}
                  className="h-10"
                  onClick={() => toggleCompare(p.id)}
                >
                  {compareIds.includes(p.id) ? "已选" : "对比"}
                </Button>
              </div>
            </div>
          ))}
        </div>
        <button
          onClick={() => openAgent("根据我的风险测评，推荐几只适合我的理财产品并对比优劣")}
          className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-primary/40 bg-primary/5 py-3.5 text-sm font-medium text-primary transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Sparkles className="h-4 w-4" />
          让小犀按我的风险等级推荐
        </button>
      </section>

      {/* 对比弹窗 */}
      <Dialog open={compareOpen} onOpenChange={setCompareOpen}>
        <DialogContent className="mx-auto max-w-md">
          <DialogHeader>
            <DialogTitle>产品对比</DialogTitle>
          </DialogHeader>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b text-left text-xs text-muted-foreground">
                  <th className="py-2 pr-3 font-medium">指标</th>
                  {compareIds.map((id) => {
                    const p = products.data?.find((x) => x.id === id);
                    return (
                      <th key={id} className="py-2 pr-3 font-semibold text-foreground">
                        {p?.name}
                      </th>
                    );
                  })}
                </tr>
              </thead>
              <tbody className="text-xs">
                {(
                  [
                    ["业绩基准", (p: { annualRateBps: number }) => (p.annualRateBps > 0 ? `${(p.annualRateBps / 100).toFixed(2)}%` : "随市价")],
                    ["风险等级", (p: { riskLevel: string }) => p.riskLevel],
                    ["类型", (p: { type: string }) => TYPE_LABEL[p.type]],
                    ["起购金额", (p: { minAmountCents: number }) => fmtMoney(p.minAmountCents)],
                    ["期限", (p: { termDays: number }) => (p.termDays === 0 ? "灵活申赎" : `${p.termDays} 天`)],
                    ["特点", (p: { tags: string | null }) => p.tags ?? "-"],
                  ] as Array<[string, (p: NonNullable<typeof products.data>[number]) => string]>
                ).map(([label, fn]) => (
                  <tr key={label} className="border-b last:border-0">
                    <td className="py-2.5 pr-3 text-muted-foreground">{label}</td>
                    {compareIds.map((id) => {
                      const p = products.data?.find((x) => x.id === id);
                      return (
                        <td key={id} className="num py-2.5 pr-3">
                          {p ? fn(p) : "-"}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </DialogContent>
      </Dialog>

      <PurchaseSheet productId={buyId} onClose={() => setBuyId(null)} onDone={() => { utils.wealth.holdings.invalidate(); utils.account.overview.invalidate(); }} />
      <RedeemSheet holdingId={redeemId} onClose={() => setRedeemId(null)} onDone={() => { utils.wealth.holdings.invalidate(); utils.account.overview.invalidate(); }} />
      <AssessSheet open={assessOpen} onClose={() => setAssessOpen(false)} onDone={() => profile.refetch()} />
      <div className="h-4" />
    </div>
  );
}

function PurchaseSheet({ productId, onClose, onDone }: { productId: number | null; onClose: () => void; onDone: () => void }) {
  const products = trpc.wealth.products.useQuery();
  const profile = trpc.wealth.riskProfile.useQuery();
  const [amount, setAmount] = useState("");
  const p = products.data?.find((x) => x.id === productId);

  const buy = trpc.wealth.purchase.useMutation({
    onSuccess: (r) => {
      toast.success(`已申购 ${r.product}`);
      setAmount("");
      onDone();
      onClose();
    },
    onError: (e) => toast.error(e.message, { duration: 5000 }),
  });

  const submit = () => {
    const cents = Math.round(parseFloat(amount) * 100);
    if (!cents || cents <= 0) return toast.error("请输入有效金额");
    if (productId) buy.mutate({ productId, amountCents: cents });
  };

  return (
    <Sheet open={productId !== null} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>申购 · {p?.name}</SheetTitle>
        </SheetHeader>
        {p && (
          <div className="mt-4 space-y-4 pb-6">
            <div className="flex items-center justify-between rounded-xl bg-secondary/60 p-3 text-sm">
              <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${RISK_COLOR[p.riskLevel]}`}>{p.riskLevel}</span>
              <span className="text-xs text-muted-foreground">您的测评：{profile.data?.level ?? "未测评（按稳健型校验）"}</span>
            </div>
            <div>
              <Label htmlFor="buy-amount">申购金额（元，起购 {fmtMoney(p.minAmountCents)}）</Label>
              <Input id="buy-amount" inputMode="decimal" className="num mt-1.5 h-12 text-lg" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^\d.]/g, ""))} />
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">理财非存款，产品有风险，投资须谨慎。业绩比较基准不代表收益承诺。</p>
            <Button className="h-12 w-full text-base" disabled={buy.isPending} onClick={submit}>
              {buy.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              确认申购
            </Button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function RedeemSheet({ holdingId, onClose, onDone }: { holdingId: number | null; onClose: () => void; onDone: () => void }) {
  const holdingsQ = trpc.wealth.holdings.useQuery();
  const h = holdingsQ.data?.find((x) => x.id === holdingId);
  const redeem = trpc.wealth.redeem.useMutation({
    onSuccess: (r) => {
      toast.success(`已赎回 ${r.product}，本金收益已回到活期`);
      onDone();
      onClose();
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <Sheet open={holdingId !== null} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>赎回 · {h?.product?.name}</SheetTitle>
        </SheetHeader>
        {h && (
          <div className="mt-4 space-y-4 pb-6">
            <div className="rounded-xl bg-secondary/60 p-4 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">持有金额</span>
                <span className="num font-semibold">{fmtMoney(h.amountCents)}</span>
              </div>
              <div className="mt-1.5 flex justify-between">
                <span className="text-muted-foreground">累计收益</span>
                <span className="num font-semibold text-emerald-600">{fmtMoney(h.profitCents)}</span>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">全额赎回后，本金与收益将实时回到活期账户。</p>
            <Button className="h-12 w-full text-base" variant="destructive" disabled={redeem.isPending} onClick={() => redeem.mutate({ holdingId: h.id })}>
              {redeem.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              确认全额赎回
            </Button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

const QUESTIONS: Array<{ key: string; q: string; options: Array<[string, number]> }> = [
  { key: "horizon", q: "您的计划投资期限是？", options: [["6 个月以内", 5], ["6 个月-1 年", 12], ["1-3 年", 20], ["3 年以上", 28]] },
  { key: "loss", q: "您能接受的最大本金回撤是？", options: [["不能接受亏损", 5], ["5% 以内", 14], ["10% 以内", 22], ["20% 以上", 30]] },
  { key: "income", q: "您的收入稳定性如何？", options: [["不稳定", 6], ["基本稳定", 14], ["稳定且有结余", 22], ["高且持续增长", 28]] },
  { key: "exp", q: "您的投资经验？", options: [["没有经验", 4], ["只买过存款/货基", 10], ["买过债基/理财", 18], ["买过股票/权益基金", 26]] },
];

function AssessSheet({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const [answers, setAnswers] = useState<Record<string, { label: string; score: number }>>({});
  const submit = trpc.wealth.submitRiskAssessment.useMutation({
    onSuccess: (r) => {
      toast.success(`测评完成：${r.level}（${r.score} 分）`);
      setAnswers({});
      onDone();
      onClose();
    },
    onError: (e) => toast.error(e.message),
  });

  const done = QUESTIONS.every((q) => answers[q.key]);
  const submitAll = () => {
    const score = Math.min(100, Object.values(answers).reduce((s, a) => s + a.score, 0) + 10);
    const labels: Record<string, string> = {};
    for (const q of QUESTIONS) labels[q.q] = answers[q.key]!.label;
    submit.mutate({ score, answers: labels });
  };

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-h-[85dvh] max-w-md overflow-y-auto rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>风险承受能力测评</SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-5 pb-6">
          {QUESTIONS.map((q, qi) => (
            <div key={q.key}>
              <div className="text-sm font-medium">
                {qi + 1}. {q.q}
              </div>
              <div className="mt-2 grid grid-cols-2 gap-2">
                {q.options.map(([label, score]) => (
                  <button
                    key={label}
                    onClick={() => setAnswers((prev) => ({ ...prev, [q.key]: { label, score } }))}
                    className={`flex h-11 items-center justify-center rounded-lg border px-2 text-xs transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                      answers[q.key]?.label === label ? "border-primary bg-primary/10 font-medium text-primary" : "bg-card"
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
          ))}
          <Button className="h-12 w-full text-base" disabled={!done || submit.isPending} onClick={submitAll}>
            {submit.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            提交测评
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
