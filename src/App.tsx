import { useState } from "react";
import { Home, ReceiptText, Sparkles, TrendingUp, Wallet } from "lucide-react";
import { Toaster } from "@/components/ui/sonner";
import HomePage from "@/pages/HomePage";
import BillsPage from "@/pages/BillsPage";
import WealthPage from "@/pages/WealthPage";
import WalletPage from "@/pages/WalletPage";
import AgentPage from "@/pages/AgentPage";

export type TabKey = "home" | "bills" | "agent" | "wealth" | "wallet";

const TABS: Array<{ key: TabKey; label: string; icon: typeof Home }> = [
  { key: "home", label: "首页", icon: Home },
  { key: "bills", label: "账单", icon: ReceiptText },
  { key: "agent", label: "小犀", icon: Sparkles },
  { key: "wealth", label: "理财", icon: TrendingUp },
  { key: "wallet", label: "卡包", icon: Wallet },
];

export default function App() {
  const [tab, setTab] = useState<TabKey>("home");
  const [agentSeed, setAgentSeed] = useState<string | null>(null);

  const openAgent = (prompt?: string) => {
    setAgentSeed(prompt ?? null);
    setTab("agent");
  };

  return (
    <div className="min-h-dvh bg-background">
      {/* 桌面端居中成 App 栏，移动端全宽 */}
      <div className="mx-auto flex min-h-dvh w-full max-w-md flex-col bg-background shadow-[0_0_0_1px_hsl(var(--border))] lg:my-6 lg:min-h-[calc(100dvh-3rem)] lg:rounded-[28px] lg:shadow-[0_24px_60px_-24px_rgba(15,23,42,0.25)] lg:overflow-hidden">
        <main className="flex-1 overflow-y-auto pb-24">
          {tab === "home" && <HomePage openAgent={openAgent} />}
          {tab === "bills" && <BillsPage />}
          {tab === "wealth" && <WealthPage openAgent={openAgent} />}
          {tab === "wallet" && <WalletPage openAgent={openAgent} />}
          {tab === "agent" && <AgentPage seed={agentSeed} onSeedConsumed={() => setAgentSeed(null)} />}
        </main>

        {/* 底部导航 */}
        <nav className="fixed inset-x-0 bottom-0 z-40 mx-auto w-full max-w-md border-t bg-card/95 backdrop-blur safe-bottom lg:absolute">
          <div className="grid grid-cols-5">
            {TABS.map(({ key, label, icon: Icon }) => {
              const active = tab === key;
              if (key === "agent") {
                return (
                  <button
                    key={key}
                    onClick={() => setTab(key)}
                    className="group relative flex h-16 flex-col items-center justify-center gap-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
                    aria-label="小犀智能助理"
                  >
                    <span
                      className={`flex h-11 w-11 -translate-y-2 items-center justify-center rounded-full shadow-lg transition-transform duration-150 active:scale-95 ${
                        active ? "bg-primary text-primary-foreground" : "bg-primary/90 text-primary-foreground"
                      }`}
                    >
                      <Icon className="h-5 w-5" />
                    </span>
                    <span className={`absolute bottom-1.5 text-[11px] ${active ? "font-semibold text-primary" : "text-muted-foreground"}`}>
                      {label}
                    </span>
                  </button>
                );
              }
              return (
                <button
                  key={key}
                  onClick={() => setTab(key)}
                  className={`flex h-16 flex-col items-center justify-center gap-1 transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset ${
                    active ? "text-primary" : "text-muted-foreground"
                  }`}
                >
                  <Icon className="h-5 w-5" strokeWidth={active ? 2.4 : 1.8} />
                  <span className={`text-[11px] ${active ? "font-semibold" : ""}`}>{label}</span>
                </button>
              );
            })}
          </div>
        </nav>
      </div>
      <Toaster position="top-center" richColors />
    </div>
  );
}
