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

// ============ 多币种 ============
export const CURRENCY_SYMBOL: Record<string, string> = { USD: "$", EUR: "€", GBP: "£", HKD: "HK$", JPY: "JP¥", CNY: "¥" };
export const CURRENCY_NAME: Record<string, string> = { USD: "美元", EUR: "欧元", GBP: "英镑", HKD: "港币", JPY: "日元", CNY: "人民币" };

/** 外币最小单位 → 带币种符号的金额 */
export function fmtForeign(foreignCents: number, currency: string) {
  const s = (Math.abs(foreignCents) / 100).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  return `${CURRENCY_SYMBOL[currency] ?? ""}${s}`;
}

/** 汇率基点 → 4 位小数汇率 */
export const fmtRate = (bps: number) => (bps / 10000).toFixed(4);

// ============ 状态映射 ============
export const SHIPMENT_STATUS: Record<string, { label: string; cls: string }> = {
  label_created: { label: "待揽收", cls: "bg-secondary text-muted-foreground" },
  in_transit: { label: "运输中", cls: "bg-sky-50 text-sky-700" },
  customs_clearance: { label: "清关中", cls: "bg-amber-50 text-amber-700" },
  delivered: { label: "已签收", cls: "bg-emerald-50 text-emerald-700" },
  exception: { label: "异常", cls: "bg-red-50 text-red-700" },
};

export const CUSTOMS_STATUS: Record<string, { label: string; cls: string }> = {
  declared: { label: "已申报", cls: "bg-sky-50 text-sky-700" },
  inspecting: { label: "查验中", cls: "bg-amber-50 text-amber-700" },
  released: { label: "已放行", cls: "bg-emerald-50 text-emerald-700" },
  closed: { label: "已结案", cls: "bg-secondary text-muted-foreground" },
};

export const REFUND_STATUS: Record<string, { label: string; cls: string }> = {
  filing: { label: "申报中", cls: "bg-sky-50 text-sky-700" },
  reviewing: { label: "审核中", cls: "bg-amber-50 text-amber-700" },
  approved: { label: "已核准", cls: "bg-teal-50 text-teal-700" },
  paid: { label: "已到账", cls: "bg-emerald-50 text-emerald-700" },
};

export const FX_TYPE: Record<string, string> = { forward: "远期锁汇", option: "外汇期权", swap: "外汇掉期" };
export const FX_STATUS: Record<string, { label: string; cls: string }> = {
  active: { label: "存续中", cls: "bg-gold/10 text-gold" },
  settled: { label: "已交割", cls: "bg-emerald-50 text-emerald-700" },
  cancelled: { label: "已取消", cls: "bg-secondary text-muted-foreground" },
};

export const LOAN_STATUS: Record<string, { label: string; cls: string }> = {
  disbursed: { label: "在途", cls: "bg-gold/10 text-gold" },
  repaid: { label: "已结清", cls: "bg-emerald-50 text-emerald-700" },
  overdue: { label: "逾期", cls: "bg-red-50 text-red-700" },
};

export const RISK_COLOR: Record<string, string> = {
  R1: "text-emerald-700 bg-emerald-50",
  R2: "text-teal-700 bg-teal-50",
  R3: "text-amber-700 bg-amber-50",
  R4: "text-orange-700 bg-orange-50",
  R5: "text-red-700 bg-red-50",
};

export const CATEGORY_COLORS: Record<string, string> = {
  电商回款: "#0e63ff",
  物流: "#7c5cfc",
  采购: "#b45309",
  推广: "#db2777",
  关税: "#dc2626",
  仓储: "#0ea5a3",
  退税: "#059669",
  融资放款: "#0891b2",
  融资还款: "#475569",
  金融衍生品: "#c9a227",
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
