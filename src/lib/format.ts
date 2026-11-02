export const fen = (cents: number) => cents / 100;

export function fmtMoney(cents: number, opts: { sign?: boolean } = {}) {
  const v = fen(Math.abs(cents));
  const s = v.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  if (opts.sign) return `${cents >= 0 ? "+" : "-"}¥${s}`;
  return `${cents < 0 ? "-" : ""}¥${s}`;
}

export function fmtYuan(yuanVal: number) {
  return `¥${yuanVal.toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function fmtDate(d: Date | string) {
  const date = typeof d === "string" ? new Date(d) : d;
  return `${date.getMonth() + 1}月${date.getDate()}日`;
}

export function fmtDateTime(d: Date | string) {
  const date = typeof d === "string" ? new Date(d) : d;
  const hh = String(date.getHours()).padStart(2, "0");
  const mm = String(date.getMinutes()).padStart(2, "0");
  return `${date.getMonth() + 1}月${date.getDate()}日 ${hh}:${mm}`;
}

export const RISK_COLOR: Record<string, string> = {
  R1: "text-emerald-700 bg-emerald-50",
  R2: "text-teal-700 bg-teal-50",
  R3: "text-amber-700 bg-amber-50",
  R4: "text-orange-700 bg-orange-50",
  R5: "text-red-700 bg-red-50",
};

export const CATEGORY_COLORS: Record<string, string> = {
  餐饮: "#0e63ff",
  购物: "#7c5cfc",
  交通: "#0ea5a3",
  住房: "#b45309",
  医疗: "#dc2626",
  娱乐: "#db2777",
  教育: "#65a30d",
  转账: "#475569",
  理财: "#0891b2",
  工资: "#059669",
  其他: "#94a3b8",
};
