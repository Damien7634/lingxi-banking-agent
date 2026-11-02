import { useMemo, useState } from "react";
import { trpc } from "@/providers/trpc";
import { fmtMoney, CATEGORY_COLORS } from "@/lib/format";
import { PageHeader } from "@/components/common";
import { BarChart, Bar, XAxis, YAxis, ResponsiveContainer, Tooltip } from "recharts";
import { ChevronLeft, ChevronRight, ShieldAlert, TrendingDown, TrendingUp } from "lucide-react";

const NOW = new Date();

export default function BillsPage() {
  const [year, setYear] = useState(NOW.getFullYear());
  const [month, setMonth] = useState(NOW.getMonth() + 1);
  const [view, setView] = useState<"month" | "year">("month");

  const monthly = trpc.bill.monthly.useQuery({ year, month }, { enabled: view === "month" });
  const yearly = trpc.bill.yearly.useQuery({ year }, { enabled: view === "year" });
  const anomalies = trpc.bill.anomalies.useQuery({ days: 90 });

  const prevMonth = () => {
    if (month === 1) {
      setYear(year - 1);
      setMonth(12);
    } else setMonth(month - 1);
  };
  const nextMonth = () => {
    if (month === 12) {
      setYear(year + 1);
      setMonth(1);
    } else setMonth(month + 1);
  };

  const catData = useMemo(
    () =>
      (monthly.data?.topCategories ?? []).map((c) => ({
        name: c.category,
        value: c.cents / 100,
        pct: c.pct,
        color: CATEGORY_COLORS[c.category] ?? "#94a3b8",
      })),
    [monthly.data],
  );

  const yearData = useMemo(
    () =>
      (yearly.data?.months ?? []).map((m) => ({
        name: `${m.month}月`,
        支出: Math.round(m.outCents / 100),
        收入: Math.round(m.inCents / 100),
      })),
    [yearly.data],
  );

  return (
    <div>
      <PageHeader title="账单分析" sub="分类统计 · 异常识别 · 月度年度报告" />

      {/* 视图切换 */}
      <div className="mx-5 flex rounded-lg bg-secondary p-1">
        {(["month", "year"] as const).map((v) => (
          <button
            key={v}
            onClick={() => setView(v)}
            className={`h-10 flex-1 rounded-md text-sm font-medium transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
              view === v ? "bg-card shadow-sm" : "text-muted-foreground"
            }`}
          >
            {v === "month" ? "月度账单" : "年度账单"}
          </button>
        ))}
      </div>

      {view === "month" ? (
        <>
          {/* 月份切换 */}
          <div className="mx-5 mt-4 flex items-center justify-between">
            <button onClick={prevMonth} aria-label="上个月" className="flex h-11 w-11 items-center justify-center rounded-full bg-card shadow-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <ChevronLeft className="h-5 w-5" />
            </button>
            <span className="num text-base font-bold">
              {year} 年 {month} 月
            </span>
            <button onClick={nextMonth} aria-label="下个月" className="flex h-11 w-11 items-center justify-center rounded-full bg-card shadow-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <ChevronRight className="h-5 w-5" />
            </button>
          </div>

          {/* 收支概览 */}
          <section className="mx-5 mt-3 rounded-2xl bg-card p-5 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
            {monthly.isLoading ? (
              <div className="py-8 text-center text-sm text-muted-foreground">加载中…</div>
            ) : monthly.data ? (
              <>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <div className="text-xs text-muted-foreground">支出</div>
                    <div className="num mt-1 text-2xl font-bold">{fmtMoney(monthly.data.totalOutCents)}</div>
                  </div>
                  <div>
                    <div className="text-xs text-muted-foreground">收入</div>
                    <div className="num mt-1 text-2xl font-bold text-emerald-600">{fmtMoney(monthly.data.totalInCents)}</div>
                  </div>
                </div>
                {monthly.data.momPct !== null && (
                  <div className="mt-3 flex items-center gap-1.5 text-xs">
                    {monthly.data.momPct >= 0 ? (
                      <TrendingUp className="h-3.5 w-3.5 text-destructive" />
                    ) : (
                      <TrendingDown className="h-3.5 w-3.5 text-emerald-600" />
                    )}
                    <span className={monthly.data.momPct >= 0 ? "text-destructive" : "text-emerald-600"}>
                      支出环比上月 {monthly.data.momPct >= 0 ? "+" : ""}
                      {monthly.data.momPct}%
                    </span>
                    <span className="text-muted-foreground">· 共 {monthly.data.count} 笔</span>
                  </div>
                )}
              </>
            ) : (
              <div className="py-8 text-center text-sm text-muted-foreground">本月暂无数据</div>
            )}
          </section>

          {/* 分类排行 */}
          <section className="mx-5 mt-4 rounded-2xl bg-card p-5 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
            <h2 className="text-sm font-semibold">支出分类</h2>
            <div className="mt-4 space-y-3.5">
              {catData.map((c) => (
                <div key={c.name}>
                  <div className="mb-1 flex items-center justify-between text-sm">
                    <span>{c.name}</span>
                    <span className="num font-medium">
                      {fmtMoney(c.value * 100)} <span className="text-xs text-muted-foreground">{c.pct}%</span>
                    </span>
                  </div>
                  <div className="h-2 overflow-hidden rounded-full bg-secondary">
                    <div className="h-full rounded-full transition-all duration-300" style={{ width: `${Math.min(c.pct, 100)}%`, backgroundColor: c.color }} />
                  </div>
                </div>
              ))}
              {catData.length === 0 && !monthly.isLoading && <div className="py-4 text-center text-sm text-muted-foreground">本月暂无支出</div>}
            </div>
          </section>
        </>
      ) : (
        <>
          <div className="mx-5 mt-4 flex items-center justify-between">
            <button onClick={() => setYear(year - 1)} aria-label="上一年" className="flex h-11 w-11 items-center justify-center rounded-full bg-card shadow-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <ChevronLeft className="h-5 w-5" />
            </button>
            <span className="num text-base font-bold">{year} 年</span>
            <button onClick={() => setYear(year + 1)} aria-label="下一年" className="flex h-11 w-11 items-center justify-center rounded-full bg-card shadow-sm transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
              <ChevronRight className="h-5 w-5" />
            </button>
          </div>

          <section className="mx-5 mt-3 rounded-2xl bg-card p-5 shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-xs text-muted-foreground">年度支出</div>
                <div className="num mt-1 text-2xl font-bold">{yearly.data ? fmtMoney(yearly.data.totalOutCents) : "…"}</div>
              </div>
              <div>
                <div className="text-xs text-muted-foreground">年度收入</div>
                <div className="num mt-1 text-2xl font-bold text-emerald-600">{yearly.data ? fmtMoney(yearly.data.totalInCents) : "…"}</div>
              </div>
            </div>
            <div className="mt-5 h-48">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={yearData} margin={{ left: -18, right: 4, top: 4, bottom: 0 }} barGap={2}>
                  <XAxis dataKey="name" tick={{ fontSize: 10 }} tickLine={false} axisLine={false} interval={1} />
                  <YAxis tick={{ fontSize: 10 }} tickLine={false} axisLine={false} />
                  <Tooltip formatter={(v: number) => `¥${v.toLocaleString()}`} />
                  <Bar dataKey="收入" fill="#059669" radius={[3, 3, 0, 0]} />
                  <Bar dataKey="支出" fill="#0e63ff" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </section>
        </>
      )}

      {/* 异常交易 */}
      <section className="mx-5 mt-4 rounded-2xl border border-amber-200 bg-amber-50/70 p-5">
        <div className="flex items-center gap-2">
          <ShieldAlert className="h-4 w-4 text-amber-600" />
          <h2 className="text-sm font-semibold">近 90 天异常交易识别</h2>
        </div>
        <div className="mt-3 space-y-3">
          {anomalies.data?.length === 0 && <div className="text-sm text-muted-foreground">未发现异常交易，账户状态良好。</div>}
          {anomalies.data?.map((a) => (
            <div key={a.tx.id} className="rounded-xl bg-card p-3.5 shadow-sm">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">{a.tx.counterparty}</span>
                <span className="num text-sm font-bold text-destructive">-{fmtMoney(a.tx.amountCents)}</span>
              </div>
              <div className="mt-0.5 text-xs text-muted-foreground">{new Date(a.tx.occurredAt).toLocaleString("zh-CN")}</div>
              <ul className="mt-2 space-y-1">
                {a.reasons.map((r, i) => (
                  <li key={i} className="flex gap-1.5 text-xs leading-relaxed text-amber-800">
                    <span className="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-amber-500" />
                    {r}
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {anomalies.isLoading && <div className="text-sm text-muted-foreground">扫描中…</div>}
        </div>
      </section>
      <div className="h-4" />
    </div>
  );
}
