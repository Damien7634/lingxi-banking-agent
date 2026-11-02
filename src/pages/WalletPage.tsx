import { useState } from "react";
import { trpc } from "@/providers/trpc";
import { fmtMoney, fmtDateTime } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { toast } from "sonner";
import { CreditCard, Plus, Snowflake, Ban, ShieldOff, ShieldCheck, Loader2, Gift, Cake, Flower2, XCircle } from "lucide-react";

const CARD_STATUS: Record<string, { label: string; cls: string }> = {
  normal: { label: "正常", cls: "bg-emerald-50 text-emerald-700" },
  frozen: { label: "已冻结", cls: "bg-sky-50 text-sky-700" },
  lost: { label: "已挂失", cls: "bg-red-50 text-red-700" },
  restricted: { label: "交易受限", cls: "bg-amber-50 text-amber-700" },
};

const SCENARIO_STATUS: Record<string, string> = { armed: "已布防", locked: "已锁定资金", ordered: "礼品已下单", done: "已完成", cancelled: "已取消" };

type Props = { openAgent: (prompt?: string) => void };

export default function WalletPage({ openAgent }: Props) {
  return (
    <div>
      <CardsSection />
      <GuardSection openAgent={openAgent} />
      <AASection />
      <div className="h-4" />
    </div>
  );
}

// ================= 卡片管理 =================
function CardsSection() {
  const utils = trpc.useUtils();
  const cardsQ = trpc.card.list.useQuery();
  const [limitCard, setLimitCard] = useState<number | null>(null);

  const invalidate = () => utils.card.list.invalidate();
  const mut = trpc.card.setStatus.useMutation({
    onSuccess: (r) => {
      toast.success(r.message);
      invalidate();
    },
    onError: (e) => toast.error(e.message),
  });
  const toggle = trpc.card.toggleOnlinePay.useMutation({ onSuccess: invalidate, onError: (e) => toast.error(e.message) });
  const apply = trpc.card.applyCard.useMutation({
    onSuccess: (r) => {
      toast.success(`${r.message}，卡号尾号 ${r.last4}`);
      invalidate();
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <section className="mx-5">
      <div className="flex items-center justify-between px-1">
        <h2 className="text-sm font-semibold">我的银行卡</h2>
        <button
          onClick={() => apply.mutate({ cardType: "credit" })}
          className="flex h-9 items-center gap-1 rounded-full bg-secondary px-3 text-xs font-medium transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Plus className="h-3.5 w-3.5" />
          申请新卡
        </button>
      </div>
      <div className="mt-3 space-y-3">
        {cardsQ.data?.map((c) => {
          const st = CARD_STATUS[c.status];
          return (
            <div key={c.id} className={`rounded-2xl p-5 text-white shadow-md ${c.cardType === "credit" ? "bg-slate-800" : "bg-primary"}`}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <CreditCard className="h-4 w-4 opacity-80" />
                  <span className="text-sm font-medium">{c.bankName} · {c.cardType === "credit" ? "信用卡" : "借记卡"}</span>
                </div>
                <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${st.cls}`}>{st.label}</span>
              </div>
              <div className="num mt-3 text-lg font-semibold tracking-[0.2em]">**** **** **** {c.last4}</div>
              {c.cardType === "credit" && (
                <div className="num mt-2 text-xs opacity-85">
                  额度 {fmtMoney(c.creditLimitCents ?? 0)} · 已用 {fmtMoney(c.usedCents)}
                </div>
              )}
              <div className="num mt-0.5 text-xs opacity-85">单日限额 {fmtMoney(c.dailyLimitCents)}</div>

              <div className="mt-4 grid grid-cols-4 gap-2">
                {c.status !== "lost" && (
                  <>
                    <CardAction
                      label={c.status === "frozen" ? "解冻" : "冻结"}
                      icon={<Snowflake className="h-3.5 w-3.5" />}
                      onClick={() => mut.mutate({ cardId: c.id, action: c.status === "frozen" ? "unfreeze" : "freeze" })}
                    />
                    <CardAction
                      label={c.status === "restricted" ? "解除限制" : "限制交易"}
                      icon={c.status === "restricted" ? <ShieldCheck className="h-3.5 w-3.5" /> : <ShieldOff className="h-3.5 w-3.5" />}
                      onClick={() => mut.mutate({ cardId: c.id, action: c.status === "restricted" ? "unrestrict" : "restrict" })}
                    />
                    <CardAction label="调额" icon={<CreditCard className="h-3.5 w-3.5" />} onClick={() => setLimitCard(c.id)} />
                  </>
                )}
                <CardAction
                  label="挂失"
                  danger
                  icon={<Ban className="h-3.5 w-3.5" />}
                  onClick={() => {
                    if (window.confirm(`确认挂失尾号 ${c.last4} 的卡片？挂失后该卡将立即停止所有交易。`)) {
                      mut.mutate({ cardId: c.id, action: "reportLost" });
                    }
                  }}
                />
              </div>
              <div className="mt-3 flex items-center justify-between border-t border-white/15 pt-3">
                <span className="text-xs opacity-85">线上支付</span>
                <Switch
                  checked={c.onlinePayEnabled}
                  onCheckedChange={(v) => toggle.mutate({ cardId: c.id, enabled: v })}
                  disabled={c.status === "lost"}
                  aria-label="线上支付开关"
                />
              </div>
            </div>
          );
        })}
        {cardsQ.isLoading && <div className="py-6 text-center text-sm text-muted-foreground">加载中…</div>}
      </div>
      <LimitSheet cardId={limitCard} onClose={() => setLimitCard(null)} />
    </section>
  );
}

function CardAction({ label, icon, onClick, danger }: { label: string; icon: React.ReactNode; onClick: () => void; danger?: boolean }) {
  return (
    <button
      onClick={onClick}
      className={`flex h-11 flex-col items-center justify-center gap-0.5 rounded-lg text-[11px] font-medium transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white/60 ${
        danger ? "bg-white/10 text-red-200 hover:bg-white/20" : "bg-white/10 hover:bg-white/20"
      }`}
    >
      {icon}
      {label}
    </button>
  );
}

function LimitSheet({ cardId, onClose }: { cardId: number | null; onClose: () => void }) {
  const utils = trpc.useUtils();
  const cardsQ = trpc.card.list.useQuery();
  const card = cardsQ.data?.find((c) => c.id === cardId);
  const [daily, setDaily] = useState("");
  const [credit, setCredit] = useState("");
  const mut = trpc.card.adjustLimit.useMutation({
    onSuccess: () => {
      toast.success("额度已调整");
      utils.card.list.invalidate();
      setDaily("");
      setCredit("");
      onClose();
    },
    onError: (e) => toast.error(e.message),
  });

  const submit = () => {
    const input: { cardId: number; dailyLimitCents?: number; creditLimitCents?: number } = { cardId: cardId! };
    if (daily) input.dailyLimitCents = Math.round(parseFloat(daily) * 100);
    if (credit && card?.cardType === "credit") input.creditLimitCents = Math.round(parseFloat(credit) * 100);
    if (!input.dailyLimitCents && !input.creditLimitCents) return toast.error("请至少填写一项额度");
    mut.mutate(input);
  };

  return (
    <Sheet open={cardId !== null} onOpenChange={(v) => !v && onClose()}>
      <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
        <SheetHeader>
          <SheetTitle>调整额度 · 尾号 {card?.last4}</SheetTitle>
        </SheetHeader>
        <div className="mt-4 space-y-4 pb-6">
          <div>
            <Label htmlFor="lim-daily">单日交易限额（元，当前 {card ? fmtMoney(card.dailyLimitCents) : "-"}）</Label>
            <Input id="lim-daily" inputMode="decimal" className="num mt-1.5 h-12" placeholder="不修改则留空" value={daily} onChange={(e) => setDaily(e.target.value.replace(/[^\d.]/g, ""))} />
          </div>
          {card?.cardType === "credit" && (
            <div>
              <Label htmlFor="lim-credit">信用额度（元，当前 {fmtMoney(card.creditLimitCents ?? 0)}）</Label>
              <Input id="lim-credit" inputMode="decimal" className="num mt-1.5 h-12" placeholder="不修改则留空" value={credit} onChange={(e) => setCredit(e.target.value.replace(/[^\d.]/g, ""))} />
            </div>
          )}
          <Button className="h-12 w-full text-base" disabled={mut.isPending} onClick={submit}>
            {mut.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            确认调整
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ================= 生日守护 =================
function GuardSection({ openAgent }: { openAgent: (p?: string) => void }) {
  const utils = trpc.useUtils();
  const scenarios = trpc.scenario.list.useQuery();
  const contacts = trpc.transfer.listContacts.useQuery();
  const [createOpen, setCreateOpen] = useState(false);
  const [contactId, setContactId] = useState<number | null>(null);
  const [lockYuan, setLockYuan] = useState("1000");
  const [leadDays, setLeadDays] = useState("2");

  const create = trpc.scenario.createBirthdayGuard.useMutation({
    onSuccess: (r) => {
      toast.success(`已为 ${r.contact.name} 开启生日守护`);
      utils.scenario.list.invalidate();
      utils.account.overview.invalidate();
      setCreateOpen(false);
    },
    onError: (e) => toast.error(e.message),
  });
  const cancel = trpc.scenario.cancel.useMutation({
    onSuccess: () => {
      toast.success("守护任务已取消，锁定资金已解锁");
      utils.scenario.list.invalidate();
      utils.account.overview.invalidate();
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <section className="mx-5 mt-6">
      <div className="flex items-center justify-between px-1">
        <h2 className="text-sm font-semibold">跨场景守护</h2>
        <button
          onClick={() => setCreateOpen(true)}
          className="flex h-9 items-center gap-1 rounded-full bg-secondary px-3 text-xs font-medium transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Gift className="h-3.5 w-3.5" />
          新建守护
        </button>
      </div>

      <div className="mt-3 space-y-3">
        {scenarios.data?.length === 0 && (
          <div className="rounded-2xl border border-dashed p-6 text-center text-sm text-muted-foreground">
            还没有守护任务。比如：「我爱人生日时，当月锁定 1000 元，提前 2 天自动订鲜花和蛋糕」。
            <button onClick={() => openAgent("帮我给爱人设置生日守护，当月锁定1000元，提前2天订鲜花和蛋糕")} className="mt-3 inline-flex h-10 items-center gap-1.5 rounded-full bg-primary px-4 text-xs font-medium text-primary-foreground transition active:scale-95">
              让小犀帮我设置
            </button>
          </div>
        )}
        {scenarios.data?.map((s) => {
          const steps = ["armed", "locked", "ordered", "done"] as const;
          const stepIdx = s.status === "cancelled" ? -1 : steps.indexOf(s.status as (typeof steps)[number]);
          const stepLabels = ["布防", "锁款", "下单", "完成"];
          return (
            <div key={s.id} className="rounded-2xl bg-card p-4 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
              <div className="flex items-center justify-between">
                <div className="text-sm font-semibold">{s.title}</div>
                <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${s.status === "cancelled" ? "bg-secondary text-muted-foreground" : "bg-pink-50 text-pink-700"}`}>
                  {SCENARIO_STATUS[s.status]}
                </span>
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                {s.contact?.name} · 生日 {s.payload.birthday} · 锁定 {fmtMoney(s.payload.lockCents)} · 提前 {s.payload.orderLeadDays} 天下单
              </div>

              {/* 进度 */}
              {s.status !== "cancelled" && (
                <div className="mt-3 flex items-center">
                  {stepLabels.map((label, i) => (
                    <div key={label} className="flex flex-1 items-center last:flex-none">
                      <div className="flex flex-col items-center gap-1">
                        <span className={`h-2.5 w-2.5 rounded-full ${i <= stepIdx ? "bg-primary" : "bg-secondary"} ${i === stepIdx && s.status !== "done" ? "pulse-dot" : ""}`} />
                        <span className={`text-[10px] ${i <= stepIdx ? "font-medium text-primary" : "text-muted-foreground"}`}>{label}</span>
                      </div>
                      {i < stepLabels.length - 1 && <div className={`mx-1 mb-4 h-px flex-1 ${i < stepIdx ? "bg-primary" : "bg-secondary"}`} />}
                    </div>
                  ))}
                </div>
              )}

              {/* 礼品订单 */}
              {s.orders.length > 0 && (
                <div className="mt-3 space-y-1.5">
                  {s.orders.map((o) => (
                    <div key={o.id} className="flex items-center gap-2 rounded-lg bg-secondary/50 px-3 py-2 text-xs">
                      {o.kind === "flower" ? <Flower2 className="h-3.5 w-3.5 text-pink-600" /> : <Cake className="h-3.5 w-3.5 text-amber-600" />}
                      <span className="flex-1">
                        {o.merchant} · {o.itemName}
                      </span>
                      <span className="num font-medium">{fmtMoney(o.amountCents)}</span>
                      <span className="text-muted-foreground">{{ scheduled: "待下单", placed: "已下单", delivered: "已送达", cancelled: "已取消" }[o.status]}</span>
                    </div>
                  ))}
                </div>
              )}

              {/* 日志 */}
              <div className="mt-3 space-y-1 border-l-2 border-secondary pl-3">
                {s.log.map((l, i) => (
                  <div key={i} className="text-xs leading-relaxed text-muted-foreground">
                    {l.text}
                  </div>
                ))}
              </div>

              {!["done", "cancelled"].includes(s.status) && (
                <button
                  onClick={() => window.confirm("确认取消该守护任务？锁定的资金将立即解锁。") && cancel.mutate({ id: s.id })}
                  className="mt-3 flex h-9 items-center gap-1 rounded-full px-3 text-xs font-medium text-destructive transition hover:bg-destructive/10 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <XCircle className="h-3.5 w-3.5" />
                  取消守护
                </button>
              )}
            </div>
          );
        })}
      </div>

      {/* 新建守护 */}
      <Sheet open={createOpen} onOpenChange={setCreateOpen}>
        <SheetContent side="bottom" className="mx-auto max-w-md rounded-t-2xl">
          <SheetHeader>
            <SheetTitle>新建生日守护</SheetTitle>
          </SheetHeader>
          <div className="mt-4 space-y-4 pb-6">
            <div>
              <Label>守护对象</Label>
              <div className="mt-2 flex flex-wrap gap-2">
                {contacts.data?.filter((c) => c.birthday).map((c) => (
                  <button
                    key={c.id}
                    onClick={() => setContactId(c.id)}
                    className={`h-11 rounded-full border px-4 text-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
                      contactId === c.id ? "border-primary bg-primary/10 font-medium text-primary" : "bg-card"
                    }`}
                  >
                    {c.name} <span className="text-xs text-muted-foreground">{c.birthday}</span>
                  </button>
                ))}
              </div>
            </div>
            <div>
              <Label htmlFor="gd-lock">当月锁定金额（元）</Label>
              <Input id="gd-lock" inputMode="decimal" className="num mt-1.5 h-12" value={lockYuan} onChange={(e) => setLockYuan(e.target.value.replace(/[^\d.]/g, ""))} />
            </div>
            <div>
              <Label htmlFor="gd-lead">生日前几天下单礼品</Label>
              <Input id="gd-lead" inputMode="numeric" className="num mt-1.5 h-12" value={leadDays} onChange={(e) => setLeadDays(e.target.value.replace(/\D/g, "").slice(0, 1))} />
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">进入生日当月自动锁定活期专款；生日前 N 天自动下单鲜花（花加）和蛋糕（诺心）；生日当天礼品送达、余款自动解锁。</p>
            <Button
              className="h-12 w-full text-base"
              disabled={create.isPending}
              onClick={() => {
                const c = contacts.data?.find((x) => x.id === contactId);
                if (!c) return toast.error("请选择守护对象");
                create.mutate({ contactQuery: c.name, lockCents: Math.round(parseFloat(lockYuan || "0") * 100), orderLeadDays: parseInt(leadDays || "2", 10) });
              }}
            >
              {create.isPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              开启守护
            </Button>
          </div>
        </SheetContent>
      </Sheet>
    </section>
  );
}

// ================= AA 收款管理 =================
function AASection() {
  const utils = trpc.useUtils();
  const splits = trpc.transfer.listAASplits.useQuery();
  const mark = trpc.transfer.markAAPaid.useMutation({
    onSuccess: (r) => {
      toast.success(r.done ? "全部收齐，AA 完成" : "已标记到账");
      utils.transfer.listAASplits.invalidate();
    },
    onError: (e) => toast.error(e.message),
  });

  if ((splits.data?.length ?? 0) === 0 && !splits.isLoading) return null;

  return (
    <section className="mx-5 mt-6">
      <h2 className="px-1 text-sm font-semibold">AA 收款</h2>
      <div className="mt-3 space-y-3">
        {splits.data?.map((s) => (
          <div key={s.id} className="rounded-2xl bg-card p-4 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
            <div className="flex items-center justify-between">
              <div className="text-sm font-semibold">{s.title}</div>
              <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${s.status === "done" ? "bg-emerald-50 text-emerald-700" : "bg-amber-50 text-amber-700"}`}>
                {s.status === "done" ? "已收齐" : "收款中"}
              </span>
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              总额 {fmtMoney(s.totalCents)} · {fmtDateTime(s.createdAt)}
            </div>
            <div className="mt-3 space-y-1.5">
              {s.participants.map((p) => (
                <div key={p.name} className="flex items-center gap-2 text-sm">
                  <span className="flex-1">
                    {p.name} <span className="num text-muted-foreground">{fmtMoney(p.amountCents)}</span>
                  </span>
                  {p.paid ? (
                    <span className="text-xs font-medium text-emerald-600">已付</span>
                  ) : (
                    <Button size="sm" variant="outline" className="h-8 text-xs" onClick={() => mark.mutate({ id: s.id, name: p.name })}>
                      标记到账
                    </Button>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
