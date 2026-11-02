import { useState } from "react";
import { trpc } from "@/providers/trpc";
import { fmtMoney, SHIPMENT_STATUS, CUSTOMS_STATUS, REFUND_STATUS } from "@/lib/format";
import { PageHeader } from "@/components/common";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import { toast } from "sonner";
import { FileCheck2, BadgePercent, Search, Sparkles, Loader2, Package, MapPin, Plane } from "lucide-react";

type Seg = "shipments" | "customs" | "refund";
type Props = { openAgent: (prompt?: string) => void };

type ShipmentRow = {
  id: number;
  trackingNo: string;
  carrier: string;
  platform: string;
  destination: string;
  goodsDesc: string | null;
  weightKg: string | null;
  freightCents: number;
  status: string;
  milestones: Array<{ time: string; text: string; location: string }>;
  etaAt: Date | null;
};

export default function TradePage({ openAgent }: Props) {
  const ov = trpc.trade.overview.useQuery();
  const [seg, setSeg] = useState<Seg>("shipments");

  return (
    <div>
      <PageHeader title="外贸服务" sub="物流 · 海关 · 退税 一站式对接" />

      {/* 汇总驾驶舱 */}
      <section className="hero-navy mx-5 rounded-2xl p-5">
        <div className="kicker text-gold-light/80">TRADE HUB</div>
        <div className="mt-2 grid grid-cols-3 gap-2 text-center">
          <div>
            <div className="num text-xl font-bold text-gold-light">{ov.data?.activeShipments ?? "…"}</div>
            <div className="mt-0.5 text-[10px] text-white/65">在途运单</div>
          </div>
          <div className="border-x border-white/10">
            <div className="num text-xl font-bold text-gold-light">{ov.data?.pendingCustoms ?? "…"}</div>
            <div className="mt-0.5 text-[10px] text-white/65">报关在途</div>
          </div>
          <div>
            <div className="num text-xl font-bold text-gold-light">{ov.data ? fmtMoney(ov.data.refundInProgressCents).replace("¥", "¥") : "…"}</div>
            <div className="mt-0.5 text-[10px] text-white/65">退税进行中</div>
          </div>
        </div>
      </section>

      {/* 分段 */}
      <div className="mx-5 mt-4 grid grid-cols-3 gap-1 rounded-full bg-secondary/70 p-1">
        {(
          [
            ["shipments", "物流运单"],
            ["customs", "海关报关"],
            ["refund", "出口退税"],
          ] as Array<[Seg, string]>
        ).map(([key, label]) => (
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
        {seg === "shipments" && <ShipmentsSection />}
        {seg === "customs" && <CustomsSection />}
        {seg === "refund" && <RefundSection />}
      </div>

      <button
        onClick={() => openAgent("帮我汇总当前在途的外贸运单、报关单和退税进度，有异常的提醒我")}
        className="mx-5 mt-3 flex w-[calc(100%-2.5rem)] items-center justify-center gap-1.5 rounded-xl border border-dashed border-gold/50 bg-gold/5 py-3.5 text-sm font-medium text-gold transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Sparkles className="h-4 w-4" />
        让小犀汇总我的外贸单证
      </button>
      <div className="h-4" />
    </div>
  );
}

// ================= 物流运单 =================
function ShipmentsSection() {
  const shipments = trpc.trade.shipments.useQuery();
  const [trackNo, setTrackNo] = useState("");
  const [detail, setDetail] = useState<ShipmentRow | null>(null);

  const track = trpc.trade.track.useQuery(
    { trackingNo: trackNo },
    { enabled: false },
  );

  const doTrack = async () => {
    if (trackNo.trim().length < 4) return toast.error("请输入至少 4 位运单号");
    const r = await track.refetch();
    if (r.data) {
      setDetail(r.data as ShipmentRow);
    } else {
      toast.error(r.error?.message ?? "未找到该运单");
    }
  };

  return (
    <div className="space-y-3">
      {/* 单号查询 */}
      <div className="mx-5 flex gap-2">
        <Input
          value={trackNo}
          onChange={(e) => setTrackNo(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && doTrack()}
          placeholder="输入运单号查询轨迹，如 YT88362012"
          className="h-12 flex-1"
        />
        <button
          onClick={doTrack}
          disabled={track.isFetching}
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-navy text-gold-light transition hover:bg-navy-light active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label="查询运单"
        >
          {track.isFetching ? <Loader2 className="h-4.5 w-4.5 animate-spin" /> : <Search className="h-4.5 w-4.5" />}
        </button>
      </div>

      <div className="mx-5 space-y-3">
        {shipments.data?.map((s) => {
          const st = SHIPMENT_STATUS[s.status];
          const latest = s.milestones[s.milestones.length - 1];
          return (
            <button
              key={s.id}
              onClick={() => setDetail(s)}
              className="card-in w-full rounded-2xl bg-card p-4 text-left card-elevated transition active:scale-[0.99] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Package className="h-4 w-4 text-navy-light" />
                  <span className="text-sm font-semibold">{s.carrier}</span>
                  <span className="num text-xs text-muted-foreground">{s.trackingNo}</span>
                </div>
                <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${st.cls}`}>{st.label}</span>
              </div>
              <div className="mt-1.5 text-xs text-muted-foreground">
                {s.platform} → {s.destination} · {s.goodsDesc} · {s.weightKg}
              </div>
              {latest && (
                <div className="mt-2 flex items-start gap-1.5 rounded-lg bg-secondary/50 px-2.5 py-2 text-xs">
                  <MapPin className="mt-0.5 h-3 w-3 shrink-0 text-navy-light" />
                  <span className="leading-relaxed">
                    <span className="num text-muted-foreground">{latest.time}</span> {latest.text}
                  </span>
                </div>
              )}
              <div className="num mt-1.5 text-[11px] text-muted-foreground">
                运费 {fmtMoney(s.freightCents)}
                {s.etaAt && s.status !== "delivered" && <> · 预计 {new Date(s.etaAt).getMonth() + 1}月{new Date(s.etaAt).getDate()}日到达</>}
              </div>
            </button>
          );
        })}
        {shipments.isLoading && <div className="py-6 text-center text-sm text-muted-foreground">加载中…</div>}
      </div>

      {/* 轨迹详情 Sheet */}
      <Sheet open={detail !== null} onOpenChange={(v) => !v && setDetail(null)}>
        <SheetContent side="bottom" className="mx-auto max-h-[85dvh] max-w-md overflow-y-auto rounded-t-2xl">
          <SheetHeader>
            <SheetTitle>运单轨迹 · {detail?.trackingNo}</SheetTitle>
          </SheetHeader>
          {detail && (
            <div className="mt-4 pb-6">
              <div className="rounded-xl bg-secondary/60 p-3 text-xs text-muted-foreground">
                {detail.carrier} · {detail.platform} → {detail.destination} · {detail.goodsDesc}
              </div>
              <div className="mt-4 space-y-0">
                {[...detail.milestones].reverse().map((m, i) => (
                  <div key={i} className="relative flex gap-3 pb-5 pl-1">
                    <div className="flex flex-col items-center">
                      <span className={`z-10 h-2.5 w-2.5 rounded-full ${i === 0 ? "bg-gold pulse-dot" : "bg-navy/30"}`} />
                      {i < detail.milestones.length - 1 && <span className="absolute top-3 h-full w-px bg-navy/15" />}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className={`text-sm leading-snug ${i === 0 ? "font-medium" : "text-muted-foreground"}`}>{m.text}</div>
                      <div className="num mt-0.5 text-[11px] text-muted-foreground">{m.time} · {m.location}</div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}

// ================= 海关报关 =================
function CustomsSection() {
  const customs = trpc.trade.customs.useQuery();
  return (
    <div className="mx-5 space-y-3">
      {customs.data?.map((d) => {
        const st = CUSTOMS_STATUS[d.status];
        return (
          <div key={d.id} className="card-in rounded-2xl bg-card p-4 card-elevated">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <FileCheck2 className="h-4 w-4 text-navy-light" />
                <span className="num text-sm font-semibold">{d.declNo}</span>
              </div>
              <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${st.cls}`}>{st.label}</span>
            </div>
            <div className="mt-1.5 text-xs text-muted-foreground">
              {d.port} · {d.goodsDesc}
            </div>
            <div className="num mt-2 flex items-center justify-between text-xs">
              <span className="text-muted-foreground">
                申报 {new Date(d.declaredAt).getMonth() + 1}月{new Date(d.declaredAt).getDate()}日
                {d.releasedAt && <> · 放行 {new Date(d.releasedAt).getMonth() + 1}月{new Date(d.releasedAt).getDate()}日</>}
              </span>
              <span className="font-semibold">货值 ${(d.valueUsdCents / 100).toLocaleString()}</span>
            </div>
            {d.refund && (
              <div className="mt-2 flex items-center gap-1.5 rounded-lg bg-accent/60 px-2.5 py-2 text-xs text-accent-foreground">
                <BadgePercent className="h-3.5 w-3.5" />
                关联退税 {d.refund.period} 期 {fmtMoney(d.refund.amountCents)}（{REFUND_STATUS[d.refund.status].label} {d.refund.progress}%）
              </div>
            )}
          </div>
        );
      })}
      {customs.isLoading && <div className="py-6 text-center text-sm text-muted-foreground">加载中…</div>}
    </div>
  );
}

// ================= 出口退税 =================
function RefundSection() {
  const refunds = trpc.trade.taxRefunds.useQuery();
  return (
    <div className="mx-5 space-y-3">
      {refunds.data?.map((r) => {
        const st = REFUND_STATUS[r.status];
        return (
          <div key={r.id} className="card-in rounded-2xl bg-card p-4 card-elevated">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Plane className="h-4 w-4 rotate-45 text-navy-light" />
                <span className="text-sm font-semibold">{r.period} 期出口退税</span>
              </div>
              <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${st.cls}`}>{st.label}</span>
            </div>
            <div className="num mt-2 text-lg font-bold text-gold">{fmtMoney(r.amountCents)}</div>
            <div className="mt-2">
              <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                <span>办理进度</span>
                <span className="num font-semibold text-foreground">{r.progress}%</span>
              </div>
              <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-secondary">
                <div className="h-full rounded-full bg-gradient-to-r from-navy to-gold transition-all duration-500" style={{ width: `${r.progress}%` }} />
              </div>
            </div>
            {r.declNo && <div className="num mt-2 text-[11px] text-muted-foreground">关联报关单 {r.declNo}</div>}
          </div>
        );
      })}
      {refunds.isLoading && <div className="py-6 text-center text-sm text-muted-foreground">加载中…</div>}
      <p className="px-1 text-[11px] leading-relaxed text-muted-foreground">
        退税在途资金可申请「出口退税贷」提前回笼（金融 → 供应链金融），年化利率 5.6% 起。
      </p>
    </div>
  );
}
