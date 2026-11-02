import { useEffect, useState } from "react";
import { trpc } from "@/providers/trpc";
import {
  fmtMoney,
  fmtForeign,
  RISK_COLOR,
  FX_TYPE,
  FX_STATUS,
  LOAN_STATUS,
  CURRENCY_NAME,
} from "@/lib/format";
import { PageHeader } from "@/components/common";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { toast } from "sonner";
import { GitCompareArrows, Loader2, Sparkles, ShieldCheck, HandCoins, Landmark } from "lucide-react";
import type { FinanceSeg } from "@/App";

const TYPE_LABEL: Record<string, string> = { cash: "现金管理", bond: "固收", mixed: "混合", index: "指数", gold: "黄金" };

const SEGS: Array<{ key: FinanceSeg; label: string; icon: typeof ShieldCheck }> = [
  { key: "fx", label: "汇率避险", icon: ShieldCheck },
  { key: "scf", label: "供应链金融", icon: HandCoins },
  { key: "wealth", label: "理财", icon: Landmark },
];

type Props = { openAgent: (prompt?: string) => void; initialSeg: FinanceSeg };

export default function FinancePage({ openAgent, initialSeg }: Props) {
  const [seg, setSeg] = useState<FinanceSeg>(initialSeg);
  useEffect(() => setSeg(initialSeg), [initialSeg]);

  return (
    <div>
      <PageHeader title="跨境金融" sub="汇率避险 · 供应链金融 · 财富增值" />

      {/* 分段控件 */}
      <div className="mx-5 grid grid-cols-3 gap-1 rounded-full bg-secondary/70 p-1">
        {SEGS.map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setSeg(key)}
            className={`flex h-10 items-center justify-center rounded-full text-[13px] font-medium transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
              seg === key ? "bg-navy text-gold-light shadow-sm" : "text-muted-foreground"
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="mt-4">
        {seg === "fx" && <FxSection openAgent={openAgent} />}
        {seg === "scf" && <ScfSection openAgent={openAgent} />}
        {seg === "wealth" && <WealthSection openAgent={openAgent} />}
      </div>
      <div className="h-4" />
    </div>
  );
}

// ================= 汇率避险 =================
function FxSection({ openAgent }: { openAgent: (p?: string) => void }) {
  const utils = trpc.useUtils();
  const rates = trpc.fx.rates.useQuery();
  const exposure = trpc.fx.exposure.useQuery();
  const contracts = trpc.fx.contracts.useQuery();
  const [hedgeOpen, setHedgeOpen] = useState(false);

  const cancel = trpc.fx.cancel.useMutation({
    onSuccess: (r) => {
      toast.success(r.message);
      utils.fx.contracts.invalidate();
      utils.fx.exposure.invalidate();
      utils.account.overview.invalidate();
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <div className="space-y-3">
      {/* 敞口分析 */}
      <section className="hero-navy mx-5 rounded-2xl p-5">
        <div className="kicker text-gold-light/80">FX EXPOSURE</div>
        <div className="mt-1 text-sm font-semibold">汇率风险敞口</div>
        {exposure.data ? (
          <>
            <div className="num mt-2 text-2xl font-bold text-gold-light">{fmtMoney(exposure.data.totalCnyCents)}</div>
            <div className="mt-2">
              <div className="flex items-center justify-between text-[11px] text-white/70">
                <span>已对冲 {fmtMoney(exposure.data.totalHedgedCnyCents)}</span>
                <span className="num font-semibold text-gold-light">{Math.round(exposure.data.hedgeRatio * 100)}%</span>
              </div>
              <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-white/15">
                <div className="h-full rounded-full bg-gradient-to-r from-gold to-gold-light transition-all duration-500" style={{ width: `${Math.round(exposure.data.hedgeRatio * 100)}%` }} />
              </div>
            </div>
            <div className="mt-3 space-y-1.5">
              {exposure.data.items.map((it) => (
                <div key={it.currency} className="flex items-center justify-between text-xs">
                  <span className="text-white/80">{it.name} {it.currency}</span>
                  <span className="num text-white/90">
                    {fmtForeign(it.foreignCents, it.currency)} <span className="text-white/50">≈{fmtMoney(it.cnyCents)}</span>
                  </span>
                </div>
              ))}
            </div>
            <p className="mt-3 text-[11px] leading-relaxed text-white/60">
              未对冲敞口 {fmtMoney(exposure.data.openCnyCents)}：人民币升值将侵蚀回款利润。建议对确定回款做远期锁汇，不确定时点用期权保留弹性。
            </p>
          </>
        ) : (
          <div className="py-4 text-sm text-white/60">分析中…</div>
        )}
      </section>

      {/* 牌价 */}
      <section className="mx-5 rounded-2xl bg-card p-4 card-elevated">
        <div className="flex items-center justify-between px-1">
          <h2 className="text-sm font-semibold">实时牌价</h2>
          <span className="text-[10px] text-muted-foreground">每 10 分钟刷新</span>
        </div>
        <div className="mt-2 divide-y">
          {rates.data?.map((r) => (
            <div key={r.pair} className="flex items-center justify-between py-2.5">
              <div>
                <div className="text-sm font-medium">{r.name} <span className="text-xs text-muted-foreground">{r.currency}</span></div>
              </div>
              <div className="flex gap-4 text-right">
                <div>
                  <div className="num text-sm font-semibold text-gold">{r.buyRate.toFixed(4)}</div>
                  <div className="text-[10px] text-muted-foreground">结汇价</div>
                </div>
                <div>
                  <div className="num text-sm font-semibold">{r.sellRate.toFixed(4)}</div>
                  <div className="text-[10px] text-muted-foreground">购汇价</div>
                </div>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* 我的合约 */}
      <section className="mx-5">
        <div className="flex items-center justify-between px-1">
          <h2 className="text-sm font-semibold">我的避险合约</h2>
          <button
            onClick={() => setHedgeOpen(true)}
            className="flex h-9 items-center gap-1 rounded-full bg-gold px-3.5 text-xs font-semibold text-navy transition hover:bg-gold-light active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <ShieldCheck className="h-3.5 w-3.5" />
            新建避险
          </button>
        </div>
        <div className="mt-2 space-y-2.5">
          {contracts.data?.length === 0 && (
            <div className="rounded-2xl border border-dashed p-6 text-center text-sm text-muted-foreground">暂无合约，点击「新建避险」锁定第一笔汇率</div>
          )}
          {contracts.data?.map((c) => {
            const st = FX_STATUS[c.status];
            return (
              <div key={c.id} className="rounded-2xl bg-card p-4 card-elevated">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-semibold">{FX_TYPE[c.type]}</span>
                    <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${st.cls}`}>{st.label}</span>
                  </div>
                  <span className="num text-sm font-semibold">{CURRENCY_NAME[c.currency]} {fmtForeign(c.foreignAmountCents, c.currency)}</span>
                </div>
                <div className="num mt-1.5 text-xs text-muted-foreground">
                  锁定汇率 <span className="font-semibold text-gold">{(c.lockedRateBps / 10000).toFixed(4)}</span>
                  {" · "}到期 {new Date(c.expiresAt).getMonth() + 1}月{new Date(c.expiresAt).getDate()}日
                  {c.marginCents > 0 && <>{" · "}保证金 {fmtMoney(c.marginCents)}</>}
                  {c.premiumCents > 0 && <>{" · "}期权费 {fmtMoney(c.premiumCents)}</>}
                </div>
                {c.status === "active" && c.type === "forward" && (
                  <button
                    onClick={() => window.confirm("确认取消该远期合约？保证金将退回活期。") && cancel.mutate({ id: c.id })}
                    className="mt-2.5 flex h-9 items-center rounded-full px-3 text-xs font-medium text-destructive transition hover:bg-destructive/10 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    取消合约
                  </button>
                )}
              </div>
            );
          })}
        </div>
        <button
          onClick={() => openAgent("帮我分析一下当前的汇率风险敞口，并给出锁汇或期权建议")}
          className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-gold/50 bg-gold/5 py-3.5 text-sm font-medium text-gold transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Sparkles className="h-4 w-4" />
          让小犀分析我的汇率敞口
        </button>
      </section>

      <HedgeSheet open={hedgeOpen} onClose={() => setHedgeOpen(false)} />
    </div>
  );
}

// ============ 新建避险（远期/期权） ============
function HedgeSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const utils = trpc.useUtils();
  const wallets = trpc.fx.wallets.useQuery(undefined, { enabled: open });
  const [kind, setKind] = useState<"forward" | "option">("forward");
  const [currency, setCurrency] = useState("USD");
  const [amount, setAmount] = useState("");
  const [tenor, setTenor] = useState(30);

  const done = (msg: string) => {
    toast.success(msg, { duration: 5000 });
    utils.fx.contracts.invalidate();
    utils.fx.exposure.invalidate();
    utils.account.overview.invalidate();
    setAmount("");
    onClose();
  };
  const forward = trpc.fx.createForward.useMutation({ onSuccess: (r) => done(r.message), onError: (e) => toast.error(e.message, { duration: 5000 }) });
  const option = trpc.fx.createOption.useMutation({ onSuccess: (r) => done(r.message), onError: (e) => toast.error(e.message, { duration: 5000 }) });

  const submit = () => {
    const foreignCents = Math.round(parseFloat(amount) * 100);
    if (!foreignCents || foreignCents <= 0) return toast.error("请输入外币金额");
    const input = { currency: currency as "USD" | "EUR" | "GBP" | "HKD" | "JPY", foreignAmountCents: foreignCents, tenorDays: tenor, direction: "sell_foreign" as const };
    if (kind === "forward") forward.mutate(input);
    else option.mutate(input);
  };
  const pending = forward.isPending || option.isPending;
  const wallet = wallets.data?.find((w) => w.currency === currency);

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-h-[88dvh] max-w-md overflow-y-auto rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>新建汇率避险</SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-4 pb-6">
          <div className="grid grid-cols-2 gap-2">
            {(
              [
                ["forward", "远期锁汇", "冻结 5% 保证金，到期按锁定汇率强制交割，确定性强"],
                ["option", "外汇期权", "付 1.5% 期权费，获得按约定价结汇的权利，保留上行弹性"],
              ] as const
            ).map(([k, label, desc]) => (
              <button
                key={k}
                onClick={() => setKind(k)}
                className={`rounded-xl border p-3 text-left transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                  kind === k ? "border-gold bg-gold/5" : "bg-card"
                }`}
              >
                <div className={`text-sm font-semibold ${kind === k ? "text-gold" : ""}`}>{label}</div>
                <div className="mt-1 text-[11px] leading-relaxed text-muted-foreground">{desc}</div>
              </button>
            ))}
          </div>
          <div>
            <Label>币种</Label>
            <div className="mt-2 flex flex-wrap gap-2">
              {["USD", "EUR", "GBP", "HKD", "JPY"].map((cur) => (
                <button
                  key={cur}
                  onClick={() => setCurrency(cur)}
                  className={`h-11 rounded-full border px-4 text-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    currency === cur ? "border-navy bg-navy/5 font-medium text-navy" : "bg-card"
                  }`}
                >
                  {CURRENCY_NAME[cur]} {cur}
                </button>
              ))}
            </div>
            {wallet && <div className="mt-1.5 text-xs text-muted-foreground">钱包余额 {fmtForeign(wallet.balanceForeignCents, currency)}</div>}
          </div>
          <div>
            <Label htmlFor="hg-amount">名义本金（{currency}）</Label>
            <Input id="hg-amount" inputMode="decimal" className="num mt-1.5 h-12 text-lg" placeholder="50000" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^\d.]/g, ""))} />
          </div>
          <div>
            <Label>期限</Label>
            <div className="mt-2 grid grid-cols-4 gap-2">
              {[30, 60, 90, 180].map((d) => (
                <button
                  key={d}
                  onClick={() => setTenor(d)}
                  className={`flex h-11 items-center justify-center rounded-lg border text-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    tenor === d ? "border-navy bg-navy/5 font-medium text-navy" : "bg-card"
                  }`}
                >
                  {d} 天
                </button>
              ))}
            </div>
          </div>
          <p className="text-xs leading-relaxed text-muted-foreground">
            {kind === "forward"
              ? "远期锁汇将按当前牌价锁定结汇汇率，冻结名义本金等值人民币 5% 作为保证金，到期交割后退还。"
              : "期权费一经支付不予退还；到期汇率有利可放弃行权，按市价结汇，最大损失限于期权费。"}
          </p>
          <Button className="h-12 w-full bg-gold text-base font-semibold text-navy hover:bg-gold-light" disabled={pending} onClick={submit}>
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {kind === "forward" ? "确认锁汇" : "确认购买期权"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ================= 供应链金融 =================
function ScfSection({ openAgent }: { openAgent: (p?: string) => void }) {
  const utils = trpc.useUtils();
  const products = trpc.scf.products.useQuery();
  const loans = trpc.scf.myLoans.useQuery();
  const [applyId, setApplyId] = useState<number | null>(null);

  const repay = trpc.scf.repay.useMutation({
    onSuccess: (r) => {
      toast.success(r.message, { duration: 5000 });
      utils.scf.myLoans.invalidate();
      utils.account.overview.invalidate();
      utils.bill.monthly.invalidate();
    },
    onError: (e) => toast.error(e.message, { duration: 5000 }),
  });

  const activeLoans = (loans.data ?? []).filter((l) => l.status === "disbursed");
  const historyLoans = (loans.data ?? []).filter((l) => l.status !== "disbursed");
  const totalDebt = activeLoans.reduce((s, l) => s + l.amountCents, 0);

  return (
    <div className="space-y-3">
      {/* 在途融资概览 */}
      <section className="hero-navy mx-5 rounded-2xl p-5">
        <div className="kicker text-gold-light/80">SUPPLY CHAIN FINANCE</div>
        <div className="mt-1 text-sm font-semibold">在途融资余额</div>
        <div className="num mt-2 text-2xl font-bold text-gold-light">{fmtMoney(totalDebt)}</div>
        <div className="mt-1 text-[11px] text-white/65">{activeLoans.length} 笔在途 · 按天计息 · 支持提前结清</div>
      </section>

      {/* 在途贷款 */}
      {activeLoans.length > 0 && (
        <section className="mx-5 space-y-2.5">
          {activeLoans.map((l) => (
            <div key={l.id} className="rounded-2xl bg-card p-4 card-elevated">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-semibold">{l.product?.name}</span>
                  <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${LOAN_STATUS[l.status].cls}`}>{LOAN_STATUS[l.status].label}</span>
                </div>
                <span className="num text-sm font-bold">{fmtMoney(l.amountCents)}</span>
              </div>
              <div className="num mt-1.5 text-xs text-muted-foreground">
                年化 {(l.rateBps / 100).toFixed(2)}% · {new Date(l.dueAt).getMonth() + 1}月{new Date(l.dueAt).getDate()}日到期
                {l.purpose ? ` · ${l.purpose}` : ""}
                {l.relatedRef ? ` · 凭证 ${l.relatedRef}` : ""}
              </div>
              <Button
                variant="outline"
                size="sm"
                className="mt-2.5 h-9"
                disabled={repay.isPending}
                onClick={() => window.confirm(`提前结清「${l.product?.name}」${fmtMoney(l.amountCents)}（本金+按天利息）？`) && repay.mutate({ loanId: l.id })}
              >
                {repay.isPending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                提前结清
              </Button>
            </div>
          ))}
        </section>
      )}

      {/* 产品列表 */}
      <section className="mx-5">
        <h2 className="px-1 text-sm font-semibold">跨境电商专属融资</h2>
        <div className="mt-2 space-y-3">
          {products.data?.map((p) => (
            <div key={p.id} className="rounded-2xl bg-card p-4 card-elevated">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <div className="text-sm font-semibold">{p.name}</div>
                  <div className="mt-1 flex flex-wrap gap-1">
                    {p.tags?.split(",").map((t) => (
                      <span key={t} className="rounded bg-accent px-1.5 py-0.5 text-[10px] font-medium text-accent-foreground">{t}</span>
                    ))}
                  </div>
                </div>
                <div className="shrink-0 text-right">
                  <div className="num text-lg font-bold text-gold">{(p.rateBps / 100).toFixed(2)}%</div>
                  <div className="text-[10px] text-muted-foreground">年化利率</div>
                </div>
              </div>
              <p className="mt-2 text-xs leading-relaxed text-muted-foreground">{p.description}</p>
              <div className="num mt-1.5 text-[11px] text-muted-foreground">
                额度上限 {fmtMoney(p.maxAmountCents)} · 期限 {p.termDays} 天
              </div>
              <Button size="sm" className="mt-3 h-10 w-full bg-navy text-gold-light hover:bg-navy-light" onClick={() => setApplyId(p.id)}>
                立即申请
              </Button>
            </div>
          ))}
        </div>
        <button
          onClick={() => openAgent("我是跨境电商卖家，黑五备货需要资金周转，帮我推荐合适的供应链金融产品")}
          className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-gold/50 bg-gold/5 py-3.5 text-sm font-medium text-gold transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Sparkles className="h-4 w-4" />
          让小犀帮我选融资方案
        </button>
      </section>

      {/* 历史记录 */}
      {historyLoans.length > 0 && (
        <section className="mx-5 rounded-2xl bg-card p-4 card-elevated">
          <h2 className="px-1 text-sm font-semibold">融资记录</h2>
          <div className="mt-1 divide-y">
            {historyLoans.map((l) => (
              <div key={l.id} className="flex items-center justify-between py-2.5 text-sm">
                <div>
                  <span className="font-medium">{l.product?.name}</span>
                  <span className="ml-2 text-xs text-muted-foreground">{new Date(l.appliedAt).getMonth() + 1}月{new Date(l.appliedAt).getDate()}日申请</span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="num">{fmtMoney(l.amountCents)}</span>
                  <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${LOAN_STATUS[l.status].cls}`}>{LOAN_STATUS[l.status].label}</span>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      <ScfApplySheet productId={applyId} onClose={() => setApplyId(null)} />
    </div>
  );
}

// ============ 融资申请 ============
function ScfApplySheet({ productId, onClose }: { productId: number | null; onClose: () => void }) {
  const utils = trpc.useUtils();
  const products = trpc.scf.products.useQuery();
  const p = products.data?.find((x) => x.id === productId);
  const [amount, setAmount] = useState("");
  const [purpose, setPurpose] = useState("");
  const [ref, setRef] = useState("");

  const apply = trpc.scf.applyLoan.useMutation({
    onSuccess: (r) => {
      toast.success(r.message, { duration: 5000 });
      utils.scf.myLoans.invalidate();
      utils.account.overview.invalidate();
      setAmount("");
      setPurpose("");
      setRef("");
      onClose();
    },
    onError: (e) => toast.error(e.message, { duration: 5000 }),
  });

  const submit = () => {
    const cents = Math.round(parseFloat(amount) * 100);
    if (!cents || cents <= 0) return toast.error("请输入申请金额");
    if (productId) apply.mutate({ productId, amountCents: cents, purpose: purpose || undefined, relatedRef: ref || undefined });
  };

  return (
    <Sheet open={productId !== null} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>申请 · {p?.name}</SheetTitle>
        </SheetHeader>
        {p && (
          <div className="mt-4 space-y-4 pb-6">
            <div className="num flex items-center justify-between rounded-xl bg-secondary/60 p-3 text-sm">
              <span className="text-xs text-muted-foreground">年化 {(p.rateBps / 100).toFixed(2)}% · {p.termDays} 天</span>
              <span className="text-xs text-muted-foreground">上限 {fmtMoney(p.maxAmountCents)}</span>
            </div>
            <div>
              <Label htmlFor="scf-amount">申请金额（元）</Label>
              <Input id="scf-amount" inputMode="decimal" className="num mt-1.5 h-12 text-lg" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^\d.]/g, ""))} />
            </div>
            <div>
              <Label htmlFor="scf-purpose">资金用途（可选）</Label>
              <Input id="scf-purpose" className="mt-1.5 h-12" placeholder="例如：黑五备货采购" maxLength={50} value={purpose} onChange={(e) => setPurpose(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="scf-ref">关联凭证号（可选）</Label>
              <Input id="scf-ref" className="mt-1.5 h-12" placeholder="平台订单号 / 报关单号 / 退税所属期" maxLength={64} value={ref} onChange={(e) => setRef(e.target.value)} />
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">演示环境即时审批：放款实时到账活期账户，按实际使用天数计息，支持提前结清。</p>
            <Button className="h-12 w-full bg-navy text-base font-semibold text-gold-light hover:bg-navy-light" disabled={apply.isPending} onClick={submit}>
              {apply.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              提交申请并放款
            </Button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

// ================= 理财（沿用原财富管理） =================
function WealthSection({ openAgent }: { openAgent: (p?: string) => void }) {
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
    <div className="space-y-3">
      {/* 持仓概览 */}
      <section className="hero-navy mx-5 rounded-2xl p-5">
        <div className="kicker text-gold-light/80">WEALTH</div>
        <div className="mt-1 text-xs on-navy-sub">持仓总金额（元）</div>
        <div className="num mt-1 text-2xl font-bold text-gold-light">{holdingsQ.data ? fmtMoney(totalHolding).slice(1) : "…"}</div>
        <div className="mt-1 text-xs on-navy-sub">
          累计收益 <span className="num font-semibold text-gold-light">{fmtMoney(totalProfit)}</span>
        </div>
        <div className="mt-3 flex items-center justify-between">
          <span className="rounded-full bg-white/10 px-2.5 py-1 text-[11px] text-white/85">
            风险测评：{profile.data ? `${profile.data.level}（${profile.data.score} 分）` : "未测评"}
          </span>
          <button
            onClick={() => setAssessOpen(true)}
            className="rounded-full bg-white/10 px-3 py-1.5 text-[11px] font-medium text-white/85 transition hover:bg-white/20 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
          >
            {profile.data ? "重新测评" : "立即测评"}
          </button>
        </div>
      </section>

      {/* 我的持仓 */}
      {(holdingsQ.data?.length ?? 0) > 0 && (
        <section className="mx-5 rounded-2xl bg-card p-4 card-elevated">
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
      <section className="mx-5">
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
            <div key={p.id} className="rounded-2xl bg-card p-4 card-elevated">
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
                  <div className="num text-lg font-bold text-gold">{p.annualRateBps > 0 ? `${(p.annualRateBps / 100).toFixed(2)}%` : "随市价"}</div>
                  <div className="text-[10px] text-muted-foreground">业绩比较基准</div>
                </div>
              </div>
              <p className="mt-2 text-xs leading-relaxed text-muted-foreground">{p.description}</p>
              <div className="mt-3 flex gap-2">
                <Button size="sm" className="h-10 flex-1 bg-navy text-gold-light hover:bg-navy-light" onClick={() => setBuyId(p.id)}>
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
          className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-xl border border-dashed border-gold/50 bg-gold/5 py-3.5 text-sm font-medium text-gold transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
            <Button className="h-12 w-full bg-navy text-base font-semibold text-gold-light hover:bg-navy-light" disabled={buy.isPending} onClick={submit}>
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
                      answers[q.key]?.label === label ? "border-navy bg-navy/5 font-medium text-navy" : "bg-card"
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>
          ))}
          <Button className="h-12 w-full bg-navy text-base font-semibold text-gold-light hover:bg-navy-light" disabled={!done || submit.isPending} onClick={submitAll}>
            {submit.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            提交测评
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
