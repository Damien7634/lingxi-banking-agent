import { useEffect, useRef, useState } from "react";
import { Sparkles, SendHorizonal, RotateCcw, AlertTriangle, Wrench, Loader2 } from "lucide-react";

type ToolChip = { name: string; phase: "call" | "result" };
type Msg = { role: "user" | "assistant"; content: string; tools?: ToolChip[]; error?: string };

const TOOL_LABEL: Record<string, string> = {
  getAccountOverview: "查询账户总览",
  searchPayee: "识别收款人",
  transfer: "实时转账",
  scheduleTransfer: "创建定时转账",
  createAASplit: "拆分 AA 收款",
  getMonthlyBill: "分析月度账单",
  detectAnomalies: "扫描异常交易",
  getRiskProfile: "查询风险测评",
  submitRiskAssessment: "录入风险测评",
  recommendWealth: "匹配理财产品",
  purchaseWealth: "申购理财",
  redeemWealth: "赎回理财",
  listCards: "查询银行卡",
  applyCard: "申请新卡",
  manageCard: "卡片操作",
  setupBirthdayGuard: "创建生日守护",
  listScenarios: "查询守护任务",
  getFxRates: "查询实时牌价",
  getFxWallets: "查询多币种钱包",
  getFxExposure: "分析汇率敞口",
  lockForwardRate: "远期锁汇",
  buyFxOption: "购买外汇期权",
  listFxContracts: "查询避险合约",
  listScfProducts: "查询融资产品",
  applyScfLoan: "申请供应链融资",
  repayScfLoan: "归还融资",
  listScfLoans: "查询融资记录",
  listShipments: "查询物流运单",
  trackShipment: "跟踪运单轨迹",
  listCustoms: "查询报关单",
  getTaxRefunds: "查询退税进度",
};

const SUGGESTIONS = [
  "帮我分析一下美元敞口，黑五前要不要锁汇？",
  "用亚马逊订单贷借 15 万备货，年化多少？",
  "查一下运单 YT88362012 到哪了",
  "10 月的出口退税办到哪一步了？",
  "这个月账单有没有异常交易？",
  "帮我给爱人设置生日守护",
  "给李厂长转 9.8 万货款，备注宠物用品尾款",
];

export default function AgentPage({ seed, onSeedConsumed }: { seed: string | null; onSeedConsumed: () => void }) {
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const listRef = useRef<HTMLDivElement>(null);
  const seedRef = useRef(false);

  const scrollToEnd = () => {
    requestAnimationFrame(() => {
      listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: "smooth" });
    });
  };

  const send = async (text: string) => {
    const content = text.trim();
    if (!content || busy) return;
    const history = [...msgs, { role: "user" as const, content }];
    setMsgs(history);
    setInput("");
    setBusy(true);
    scrollToEnd();

    // 占位 assistant 消息
    setMsgs((m) => [...m, { role: "assistant", content: "", tools: [] }]);

    try {
      const res = await fetch("/api/agent/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ messages: history.map((m) => ({ role: m.role, content: m.content })) }),
      });
      if (!res.ok || !res.body) {
        const errBody = await res.json().catch(() => null);
        const message = errBody?.error ?? `服务暂时不可用（HTTP ${res.status}）`;
        setMsgs((m) => {
          const copy = [...m];
          copy[copy.length - 1] = { role: "assistant", content: "", error: message };
          return copy;
        });
        return;
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let gotDone = false;

      const patch = (fn: (msg: Msg) => Msg) => {
        setMsgs((m) => {
          const copy = [...m];
          copy[copy.length - 1] = fn(copy[copy.length - 1]);
          return copy;
        });
        scrollToEnd();
      };

      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const events = buffer.split("\n\n");
        buffer = events.pop() ?? "";
        for (const raw of events) {
          const evMatch = raw.match(/^event: (.+)$/m);
          const dataMatch = raw.match(/^data: (.*)$/m);
          if (!evMatch || !dataMatch) continue;
          const event = evMatch[1];
          let data: Record<string, unknown> = {};
          try {
            data = JSON.parse(dataMatch[1]);
          } catch {
            continue;
          }
          if (event === "delta") {
            const t = String(data.text ?? "");
            patch((msg) => ({ ...msg, content: msg.content + t }));
          } else if (event === "tool") {
            const name = String(data.name ?? "");
            const phase = data.phase === "result" ? "result" : "call";
            patch((msg) => {
              const tools = [...(msg.tools ?? [])];
              const existing = tools.findIndex((x) => x.name === name && x.phase === "call");
              if (phase === "result" && existing >= 0) tools[existing] = { name, phase: "result" };
              else tools.push({ name, phase });
              return { ...msg, tools };
            });
          } else if (event === "done") {
            gotDone = true;
          } else if (event === "fatal" || event === "error") {
            patch((msg) => ({ ...msg, error: String(data.message ?? "AI 调用中断，请重试") }));
          }
        }
      }
      if (!gotDone) {
        patch((msg) => (msg.error ? msg : { ...msg, error: msg.content ? "回答可能不完整（连接中断），建议重新发送" : "连接中断，请重试" }));
      }
    } catch {
      setMsgs((m) => {
        const copy = [...m];
        copy[copy.length - 1] = { role: "assistant", content: "", error: "网络异常，请检查连接后重试" };
        return copy;
      });
    } finally {
      setBusy(false);
      scrollToEnd();
    }
  };

  // 处理从其他页面带过来的预填指令
  useEffect(() => {
    if (seed && !seedRef.current) {
      seedRef.current = true;
      onSeedConsumed();
      void send(seed);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seed]);

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-2.5 border-b px-5 py-4">
        <span className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary text-primary-foreground">
          <Sparkles className="h-4.5 w-4.5" />
        </span>
        <div className="flex-1">
          <div className="text-sm font-bold">小犀 · 银行智能助理</div>
          <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
            <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-emerald-500" />
            已连接您的账户，可直接办理业务
          </div>
        </div>
        <button
          onClick={() => setMsgs([])}
          aria-label="清空对话"
          className="flex h-11 w-11 items-center justify-center rounded-full text-muted-foreground transition hover:bg-secondary active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <RotateCcw className="h-4 w-4" />
        </button>
      </header>

      {/* 消息列表 */}
      <div ref={listRef} className="flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {msgs.length === 0 && (
          <div className="msg-in">
            <div className="rounded-2xl rounded-tl-sm bg-card p-4 text-sm leading-relaxed shadow-[0_1px_2px_rgba(15,23,42,0.05)]">
              你好，我是小犀。我可以直接帮你办业务：转账（说人名/手机号/备注都行）、定时转账、AA 收款、账单分析、异常交易提醒、理财推荐与申赎、卡片挂失/调额，还有跨场景守护——比如「我爱人生日当月锁定 1000 元，提前 2 天自动订鲜花和蛋糕」。
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
              {SUGGESTIONS.map((s) => (
                <button
                  key={s}
                  onClick={() => void send(s)}
                  className="rounded-full border bg-card px-3.5 py-2 text-left text-xs text-foreground/80 transition hover:border-primary/40 hover:text-primary active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
        )}

        {msgs.map((m, i) => (
          <div key={i} className={`msg-in flex ${m.role === "user" ? "justify-end" : "justify-start"}`}>
            <div className={`max-w-[85%] ${m.role === "user" ? "" : ""}`}>
              {(m.tools?.length ?? 0) > 0 && (
                <div className="mb-1.5 flex flex-wrap gap-1.5">
                  {m.tools!.map((t, j) => (
                    <span
                      key={j}
                      className={`flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-medium ${
                        t.phase === "call" ? "bg-amber-50 text-amber-700" : "bg-emerald-50 text-emerald-700"
                      }`}
                    >
                      {t.phase === "call" ? <Loader2 className="h-2.5 w-2.5 animate-spin" /> : <Wrench className="h-2.5 w-2.5" />}
                      {TOOL_LABEL[t.name] ?? t.name}
                    </span>
                  ))}
                </div>
              )}
              <div
                className={`whitespace-pre-wrap rounded-2xl p-3.5 text-sm leading-relaxed ${
                  m.role === "user" ? "rounded-tr-sm bg-primary text-primary-foreground" : "rounded-tl-sm bg-card shadow-[0_1px_2px_rgba(15,23,42,0.05)]"
                }`}
              >
                {m.content}
                {busy && i === msgs.length - 1 && m.role === "assistant" && !m.error && (
                  <span className="caret-blink ml-0.5 inline-block h-4 w-[2px] translate-y-0.5 bg-primary/70" />
                )}
                {!m.content && !m.error && m.role === "assistant" && (
                  <span className="text-muted-foreground">正在办理…</span>
                )}
              </div>
              {m.error && (
                <div className="mt-1.5 flex items-start gap-1.5 rounded-xl border border-destructive/30 bg-destructive/5 p-3 text-xs leading-relaxed text-destructive">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                  {m.error}
                </div>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* 输入区 */}
      <div className="border-t bg-card px-4 pb-4 pt-3 safe-bottom">
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void send(input);
          }}
          className="flex items-end gap-2"
        >
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                void send(input);
              }
            }}
            placeholder="比如：给李娜转 200，备注午饭"
            rows={1}
            aria-label="输入指令"
            className="max-h-28 min-h-[48px] flex-1 resize-none rounded-2xl border bg-background px-4 py-3 text-sm leading-relaxed outline-none transition focus:border-primary focus-visible:ring-2 focus-visible:ring-ring"
          />
          <button
            type="submit"
            disabled={busy || !input.trim()}
            aria-label="发送"
            className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground transition active:scale-95 disabled:opacity-40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {busy ? <Loader2 className="h-5 w-5 animate-spin" /> : <SendHorizonal className="h-5 w-5" />}
          </button>
        </form>
      </div>
    </div>
  );
}
