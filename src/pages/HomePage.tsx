import { useState } from "react";
import { trpc } from "@/providers/trpc";
import { fmtMoney, fmtForeign, fmtDateTime } from "@/lib/format";
import { TxItem } from "@/components/common";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { toast } from "sonner";
import {
  ArrowLeftRight,
  CalendarClock,
  Users,
  Wallet,
  Eye,
  EyeOff,
  Sparkles,
  ChevronRight,
  Loader2,
  Globe2,
  TrendingUp,
  ShieldCheck,
  HandCoins,
  Ship,
} from "lucide-react";
import type { FinanceSeg } from "@/App";

type Props = {
  openAgent: (prompt?: string) => void;
  goFinance: (seg?: FinanceSeg) => void;
  goTrade: () => void;
  openWallet: () => void;
};

export default function HomePage({ openAgent, goFinance, goTrade, openWallet }: Props) {
  const { data, isLoading } = trpc.account.overview.useQuery();
  const scenarios = trpc.scenario.list.useQuery();
  const wallets = trpc.fx.wallets.useQuery();
  const rates = trpc.fx.rates.useQuery();
  const tradeOv = trpc.trade.overview.useQuery();
  const [showBalance, setShowBalance] = useState(true);
  const [sheet, setSheet] = useState<"transfer" | "schedule" | "aa" | null>(null);

  const acc = data?.account;
  const activeScenario = scenarios.data?.find((s) => !["done", "cancelled"].includes(s.status));
  const foreignCnyCents = (wallets.data ?? []).reduce((s, w) => s + w.cnyCents, 0);
  const totalAssetsCents = (acc?.balanceCents ?? 0) + foreignCnyCents;

  return (
    <div>
      {/* ============ 深蓝 Hero：跨境驾驶舱 ============ */}
      <section className="hero-navy px-5 pb-5 pt-6 lg:rounded-t-[28px]">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-gold/15 text-gold-light">
              <Globe2 className="h-4.5 w-4.5" />
            </span>
            <div>
              <div className="kicker text-gold-light/80">LINGXI GLOBAL</div>
              <div className="text-sm font-bold leading-tight">灵犀银行 · 跨境版</div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={openWallet}
              aria-label="卡包与守护"
              className="flex h-10 w-10 items-center justify-center rounded-full bg-white/10 text-white/85 transition hover:bg-white/15 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
            >
              <Wallet className="h-4.5 w-4.5" />
            </button>
            <button
              onClick={() => openAgent()}
              className="flex h-10 items-center gap-1.5 rounded-full bg-gold px-4 text-sm font-semibold text-navy transition hover:bg-gold-light active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
            >
              <Sparkles className="h-4 w-4" />
              问小犀
            </button>
          </div>
        </div>

        {/* 总资产 */}
        <div className="mt-5">
          <div className="flex items-center gap-2">
            <span className="on-navy-sub text-xs">总资产（折人民币）</span>
            <button
              onClick={() => setShowBalance((v) => !v)}
              aria-label={showBalance ? "隐藏余额" : "显示余额"}
              className="flex h-7 w-7 items-center justify-center rounded-full on-navy-sub transition hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/70"
            >
              {showBalance ? <Eye className="h-3.5 w-3.5" /> : <EyeOff className="h-3.5 w-3.5" />}
            </button>
          </div>
          <div className="num mt-1 text-[34px] font-bold leading-tight tracking-tight text-gold-light">
            {isLoading ? "…" : showBalance ? fmtMoney(totalAssetsCents).slice(1) : "****"}
          </div>
          <div className="mt-2 flex flex-wrap gap-2 text-[11px]">
            <span className="rounded-full bg-white/10 px-2.5 py-1 text-white/85">
              活期可用 {showBalance && data ? fmtMoney(data.availableCents) : "****"}
            </span>
            {(acc?.lockedCents ?? 0) > 0 && (
              <span className="rounded-full bg-white/10 px-2.5 py-1 text-white/85">
                锁定 {showBalance ? fmtMoney(acc!.lockedCents) : "****"}（守护+保证金）
              </span>
            )}
          </div>
        </div>

        {/* 多币种钱包 */}
        <div className="mt-4 grid grid-cols-3 gap-2">
          <div className="rounded-xl bg-white/[0.07] p-3 ring-1 ring-white/10">
            <div className="text-[10px] text-white/60">CNY 活期</div>
            <div className="num mt-1 text-sm font-semibold text-white">
              {showBalance ? fmtMoney(acc?.balanceCents ?? 0) : "****"}
            </div>
          </div>
          {(wallets.data ?? []).map((w) => (
            <div key={w.currency} className="rounded-xl bg-white/[0.07] p-3 ring-1 ring-white/10">
              <div className="text-[10px] text-white/60">{w.currency} 钱包</div>
              <div className="num mt-1 text-sm font-semibold text-white">
                {showBalance ? fmtForeign(w.balanceForeignCents, w.currency) : "****"}
              </div>
              <div className="num mt-0.5 text-[10px] text-gold-light/70">≈ {showBalance ? fmtMoney(w.cnyCents) : "****"}</div>
            </div>
          ))}
        </div>
      </section>

      {/* ============ 汇率跑马灯 ============ */}
      <div className="overflow-hidden border-b bg-navy py-2">
        <div className="ticker-track flex w-max items-center gap-6 px-4">
          {[...(rates.data ?? []), ...(rates.data ?? [])].map((r, i) => (
            <span key={`${r.pair}-${i}`} className="flex shrink-0 items-baseline gap-1.5 text-[11px]">
              <span className="font-medium text-white/70">{r.pair}</span>
              <span className="num font-semibold text-gold-light">{r.buyRate.toFixed(4)}</span>
              <span className="text-white/40">结汇</span>
              <span className="num text-white/70">{r.sellRate.toFixed(4)}</span>
              <span className="text-white/40">购汇</span>
            </span>
          ))}
          {!rates.data && <span className="text-[11px] text-white/50">牌价加载中…</span>}
        </div>
      </div>

      {/* ============ 快捷操作 ============ */}
      <section className="mx-5 mt-4 grid grid-cols-5 gap-2">
        {[
          { key: "transfer" as const, label: "转账", icon: ArrowLeftRight, act: () => setSheet("transfer") },
          { key: "schedule" as const, label: "定时", icon: CalendarClock, act: () => setSheet("schedule") },
          { key: "aa" as const, label: "AA收款", icon: Users, act: () => setSheet("aa") },
          { key: "fx" as const, label: "锁汇", icon: ShieldCheck, act: () => goFinance("fx") },
          { key: "scf" as const, label: "融资", icon: HandCoins, act: () => goFinance("scf") },
        ].map(({ key, label, icon: Icon, act }) => (
          <button
            key={key}
            onClick={act}
            className="card-elevated flex h-[72px] flex-col items-center justify-center gap-1.5 rounded-xl bg-card text-[11px] font-medium transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            <Icon className="h-5 w-5 text-navy-light" />
            {label}
          </button>
        ))}
      </section>

      {/* ============ 跨境特色入口 ============ */}
      <section className="mx-5 mt-3 grid grid-cols-2 gap-2">
        <button
          onClick={() => goFinance("fx")}
          className="card-in group flex items-center gap-2.5 rounded-2xl bg-gradient-to-br from-navy to-navy-light p-4 text-left text-white transition active:scale-[0.98] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <TrendingUp className="h-5 w-5 shrink-0 text-gold-light" />
          <div className="min-w-0">
            <div className="text-sm font-semibold">汇率避险</div>
            <div className="mt-0.5 text-[11px] text-white/65">远期锁汇 · 外汇期权</div>
          </div>
        </button>
        <button
          onClick={() => goFinance("scf")}
          className="card-in group flex items-center gap-2.5 rounded-2xl bg-gradient-to-br from-gold to-[#a8861f] p-4 text-left text-navy transition active:scale-[0.98] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <HandCoins className="h-5 w-5 shrink-0" />
          <div className="min-w-0">
            <div className="text-sm font-semibold">供应链金融</div>
            <div className="mt-0.5 text-[11px] text-navy/70">订单贷 · 退税贷 · 信保融资</div>
          </div>
        </button>
      </section>

      {/* ============ 外贸服务速览 ============ */}
      {tradeOv.data && (
        <button
          onClick={goTrade}
          className="mx-5 mt-3 flex w-[calc(100%-2.5rem)] items-center gap-3 rounded-2xl bg-card p-4 text-left card-elevated transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-navy/5 text-navy">
            <Ship className="h-5 w-5" />
          </span>
          <div className="min-w-0 flex-1">
            <div className="text-sm font-semibold">外贸服务一体化</div>
            <div className="mt-0.5 truncate text-xs text-muted-foreground">
              在途运单 {tradeOv.data.activeShipments} · 报关查验 {tradeOv.data.pendingCustoms} · 退税进行中 {fmtMoney(tradeOv.data.refundInProgressCents)}
            </div>
          </div>
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
        </button>
      )}

      {/* ============ 生日守护进行中 ============ */}
      {activeScenario && (
        <button
          onClick={() => openAgent("生日守护任务现在进展如何？")}
          className="mx-5 mt-3 flex w-[calc(100%-2.5rem)] items-center gap-3 rounded-xl border border-pink-200 bg-pink-50 p-4 text-left transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span className="pulse-dot h-2.5 w-2.5 shrink-0 rounded-full bg-pink-500" />
          <div className="min-w-0 flex-1">
            <div className="text-sm font-semibold">{activeScenario.title}进行中</div>
            <div className="truncate text-xs text-muted-foreground">
              {activeScenario.contact?.name}生日 {activeScenario.payload.birthday} · 已锁定 {fmtMoney(activeScenario.payload.lockCents)}
            </div>
          </div>
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
        </button>
      )}

      {/* ============ 待执行定时转账 ============ */}
      {(data?.scheduled.length ?? 0) > 0 && (
        <section className="mx-5 mt-3 rounded-2xl bg-card p-4 card-elevated">
          <h2 className="text-sm font-semibold">待执行定时转账</h2>
          <div className="mt-2 divide-y">
            {data!.scheduled.map((s) => (
              <div key={s.id} className="flex items-center justify-between py-2.5 text-sm">
                <div>
                  <span className="font-medium">{s.contact?.name}</span>
                  <span className="ml-2 text-xs text-muted-foreground">{fmtDateTime(s.executeAt)}</span>
                </div>
                <span className="num font-semibold">{fmtMoney(s.amountCents)}</span>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* ============ 近期交易 ============ */}
      <section className="mx-5 mt-3 rounded-2xl bg-card px-4 py-3 card-elevated">
        <h2 className="py-1 text-sm font-semibold">近期交易</h2>
        <div className="divide-y">
          {data?.recent.map((t) => <TxItem key={t.id} tx={t} />)}
          {isLoading && <div className="py-6 text-center text-sm text-muted-foreground">加载中…</div>}
        </div>
      </section>

      <TransferSheet open={sheet === "transfer"} onClose={() => setSheet(null)} mode="now" />
      <TransferSheet open={sheet === "schedule"} onClose={() => setSheet(null)} mode="schedule" />
      <AASheet open={sheet === "aa"} onClose={() => setSheet(null)} />
      <div className="h-4" />
    </div>
  );
}

// ============ 转账 / 定时转账表单 ============
function TransferSheet({ open, onClose, mode }: { open: boolean; onClose: () => void; mode: "now" | "schedule" }) {
  const utils = trpc.useUtils();
  const contacts = trpc.transfer.listContacts.useQuery(undefined, { enabled: open });
  const [contactId, setContactId] = useState<number | null>(null);
  const [amount, setAmount] = useState("");
  const [memo, setMemo] = useState("");
  const [executeAt, setExecuteAt] = useState("");

  const transfer = trpc.transfer.transfer.useMutation({
    onSuccess: (r) => {
      toast.success(`已向 ${r.contact.name} 转账 ¥${r.amountText}`);
      utils.account.overview.invalidate();
      utils.bill.monthly.invalidate();
      reset();
      onClose();
    },
    onError: (e) => toast.error(e.message),
  });
  const schedule = trpc.transfer.schedule.useMutation({
    onSuccess: (r) => {
      toast.success(`定时转账已创建：${r.contact.name} ¥${r.amountText}`);
      utils.account.overview.invalidate();
      reset();
      onClose();
    },
    onError: (e) => toast.error(e.message),
  });

  const reset = () => {
    setContactId(null);
    setAmount("");
    setMemo("");
    setExecuteAt("");
  };

  const submit = () => {
    const cents = Math.round(parseFloat(amount) * 100);
    if (!contactId) return toast.error("请选择收款人");
    if (!cents || cents <= 0) return toast.error("请输入有效金额");
    if (mode === "now") {
      transfer.mutate({ contactId, amountCents: cents, memo: memo || undefined });
    } else {
      if (!executeAt) return toast.error("请选择执行时间");
      schedule.mutate({ contactId, amountCents: cents, memo: memo || undefined, executeAt: new Date(executeAt) });
    }
  };

  const pending = transfer.isPending || schedule.isPending;

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>{mode === "now" ? "转账" : "定时转账"}</SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-4 pb-6">
          <div>
            <Label>收款人</Label>
            <div className="mt-2 flex flex-wrap gap-2">
              {contacts.data?.map((c) => (
                <button
                  key={c.id}
                  onClick={() => setContactId(c.id)}
                  className={`h-11 rounded-full border px-4 text-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                    contactId === c.id ? "border-navy bg-navy/5 font-medium text-navy" : "bg-card"
                  }`}
                >
                  {c.name}
                  {c.relation ? <span className="ml-1 text-xs text-muted-foreground">·{c.relation}</span> : null}
                </button>
              ))}
            </div>
          </div>
          <div>
            <Label htmlFor="tx-amount">金额（元）</Label>
            <Input id="tx-amount" inputMode="decimal" className="num mt-1.5 h-12 text-lg" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^\d.]/g, ""))} />
          </div>
          {mode === "schedule" && (
            <div>
              <Label htmlFor="tx-time">执行时间</Label>
              <Input id="tx-time" type="datetime-local" className="mt-1.5 h-12" value={executeAt} onChange={(e) => setExecuteAt(e.target.value)} />
            </div>
          )}
          <div>
            <Label htmlFor="tx-memo">备注（可选）</Label>
            <Input id="tx-memo" className="mt-1.5 h-12" placeholder="例如：午饭" maxLength={50} value={memo} onChange={(e) => setMemo(e.target.value)} />
          </div>
          <Button className="h-12 w-full text-base" disabled={pending} onClick={submit}>
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {mode === "now" ? "确认转账" : "创建定时任务"}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ============ AA 收款表单 ============
function AASheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const utils = trpc.useUtils();
  const [title, setTitle] = useState("");
  const [total, setTotal] = useState("");
  const [names, setNames] = useState("");

  const create = trpc.transfer.createAASplit.useMutation({
    onSuccess: (r) => {
      toast.success(`AA 收款已创建：每人 ¥${r.perPerson}`);
      utils.transfer.listAASplits.invalidate();
      setTitle("");
      setTotal("");
      setNames("");
      onClose();
    },
    onError: (e) => toast.error(e.message),
  });

  const submit = () => {
    const cents = Math.round(parseFloat(total) * 100);
    const list = names
      .split(/[,，、\s]+/)
      .map((s) => s.trim())
      .filter(Boolean)
      .map((name) => ({ name }));
    if (!title.trim()) return toast.error("请填写账单名称");
    if (!cents || cents <= 0) return toast.error("请输入总金额");
    if (list.length === 0) return toast.error("请填写参与人（逗号分隔）");
    create.mutate({ title: title.trim(), totalCents: cents, participants: list });
  };

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>AA 收款</SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-4 pb-6">
          <div>
            <Label htmlFor="aa-title">账单名称</Label>
            <Input id="aa-title" className="mt-1.5 h-12" placeholder="例如：部门聚餐 · 海底捞" maxLength={40} value={title} onChange={(e) => setTitle(e.target.value)} />
          </div>
          <div>
            <Label htmlFor="aa-total">总金额（元）</Label>
            <Input id="aa-total" inputMode="decimal" className="num mt-1.5 h-12 text-lg" placeholder="0.00" value={total} onChange={(e) => setTotal(e.target.value.replace(/[^\d.]/g, ""))} />
          </div>
          <div>
            <Label htmlFor="aa-names">参与人（逗号或空格分隔，不含自己）</Label>
            <Input id="aa-names" className="mt-1.5 h-12" placeholder="王强, 陈晨, 刘洋" value={names} onChange={(e) => setNames(e.target.value)} />
          </div>
          <Button className="h-12 w-full text-base" disabled={create.isPending} onClick={submit}>
            {create.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            发起 AA 收款
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
