import { fmtMoney } from "@/lib/format";
import { ArrowDownLeft, ArrowUpRight } from "lucide-react";

export function PageHeader({ title, sub }: { title: string; sub?: string }) {
  return (
    <header className="px-5 pb-3 pt-6">
      <h1 className="text-xl font-bold tracking-tight">{title}</h1>
      {sub && <p className="mt-0.5 text-sm text-muted-foreground">{sub}</p>}
    </header>
  );
}

export function TxItem({ tx }: { tx: { id: number; direction: "in" | "out"; amountCents: number; category: string; counterparty: string; memo: string | null; occurredAt: Date; isAnomaly: boolean } }) {
  const isIn = tx.direction === "in";
  return (
    <div className="flex items-center gap-3 py-3">
      <span
        className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-full ${
          tx.isAnomaly ? "bg-destructive/10 text-destructive" : isIn ? "bg-emerald-50 text-emerald-600" : "bg-secondary text-muted-foreground"
        }`}
      >
        {isIn ? <ArrowDownLeft className="h-4 w-4" /> : <ArrowUpRight className="h-4 w-4" />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{tx.counterparty}</span>
          {tx.isAnomaly && <span className="shrink-0 rounded bg-destructive/10 px-1.5 py-0.5 text-[10px] font-medium text-destructive">异常</span>}
        </div>
        <div className="truncate text-xs text-muted-foreground">{tx.memo ? `${tx.category} · ${tx.memo}` : tx.category}</div>
      </div>
      <span className={`num shrink-0 text-sm font-semibold ${isIn ? "text-emerald-600" : "text-foreground"}`}>
        {isIn ? "+" : "-"}
        {fmtMoney(tx.amountCents)}
      </span>
    </div>
  );
}
