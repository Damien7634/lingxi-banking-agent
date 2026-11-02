import { useCallback, useEffect, useState } from "react";
import { Home, ReceiptText, Sparkles, Landmark, Ship, ArrowLeft, LogOut, Loader2 } from "lucide-react";
import LoginPage from "@/pages/LoginPage";
import { me, logout, onUnauthorized, type AuthUser } from "@/lib/auth";
import { clearQueryCache } from "@/providers/trpc";
import { Toaster } from "@/components/ui/sonner";
import HomePage from "@/pages/HomePage";
import BillsPage from "@/pages/BillsPage";
import FinancePage from "@/pages/FinancePage";
import TradePage from "@/pages/TradePage";
import WalletPage from "@/pages/WalletPage";
import AgentPage from "@/pages/AgentPage";

export type TabKey = "home" | "finance" | "agent" | "trade" | "bills";
export type FinanceSeg = "fx" | "scf" | "wealth";

const TABS: Array<{ key: TabKey; label: string; icon: typeof Home }> = [
  { key: "home", label: "首页", icon: Home },
  { key: "finance", label: "金融", icon: Landmark },
  { key: "agent", label: "小犀", icon: Sparkles },
  { key: "trade", label: "外贸", icon: Ship },
  { key: "bills", label: "账单", icon: ReceiptText },
];

export default function App() {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [authLoading, setAuthLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    me()
      .then((u) => alive && setUser(u))
      .catch(() => alive && setUser(null))
      .finally(() => alive && setAuthLoading(false));
    return () => {
      alive = false;
    };
  }, []);

  const handleSignedOut = useCallback(() => {
    clearQueryCache();
    setUser(null);
  }, []);

  useEffect(() => onUnauthorized(handleSignedOut), [handleSignedOut]);

  if (authLoading) {
    return (
      <div className="flex min-h-dvh items-center justify-center bg-background text-muted-foreground">
        <Loader2 className="h-6 w-6 animate-spin" />
      </div>
    );
  }
  if (!user) {
    return (
      <>
        <LoginPage
          onLoggedIn={(u) => {
            clearQueryCache();
            setUser(u);
          }}
        />
        <Toaster position="top-center" richColors />
      </>
    );
  }
  return (
    <AuthedApp
      onSignOut={async () => {
        await logout();
        handleSignedOut();
      }}
    />
  );
}

function AuthedApp({ onSignOut }: { onSignOut: () => void }) {
  const [tab, setTab] = useState<TabKey>("home");
  const [agentSeed, setAgentSeed] = useState<string | null>(null);
  const [financeSeg, setFinanceSeg] = useState<FinanceSeg>("fx");
  const [walletOpen, setWalletOpen] = useState(false);

  const openAgent = (prompt?: string) => {
    setAgentSeed(prompt ?? null);
    setWalletOpen(false);
    setTab("agent");
  };

  const goFinance = (seg: FinanceSeg = "fx") => {
    setFinanceSeg(seg);
    setTab("finance");
  };

  return (
    <div className="min-h-dvh bg-background">
      {/* 桌面端居中成 App 栏，移动端全宽 */}
      <div className="relative mx-auto flex min-h-dvh w-full max-w-md flex-col bg-background shadow-[0_0_0_1px_hsl(var(--border))] lg:my-6 lg:min-h-[calc(100dvh-3rem)] lg:rounded-[28px] lg:shadow-[0_24px_60px_-24px_rgba(10,37,64,0.35)] lg:overflow-hidden">
        <button
          onClick={onSignOut}
          className="absolute right-2 top-2 z-30 flex h-8 w-8 items-center justify-center rounded-full bg-black/20 text-white/80 backdrop-blur transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label="退出登录"
          title="退出登录"
        >
          <LogOut className="h-4 w-4" />
        </button>
        <main className="flex-1 overflow-y-auto pb-24">
          {tab === "home" && (
            <HomePage
              openAgent={openAgent}
              goFinance={goFinance}
              goTrade={() => setTab("trade")}
              openWallet={() => setWalletOpen(true)}
            />
          )}
          {tab === "finance" && <FinancePage openAgent={openAgent} initialSeg={financeSeg} />}
          {tab === "trade" && <TradePage openAgent={openAgent} />}
          {tab === "bills" && <BillsPage />}
          {tab === "agent" && <AgentPage seed={agentSeed} onSeedConsumed={() => setAgentSeed(null)} />}
        </main>

        {/* 卡包与守护：全屏 overlay */}
        {walletOpen && (
          <div className="fixed inset-0 z-50 mx-auto flex w-full max-w-md flex-col bg-background lg:absolute lg:rounded-[28px]">
            <div className="sticky top-0 z-10 flex h-14 items-center gap-1 border-b bg-card/95 px-2 backdrop-blur">
              <button
                onClick={() => setWalletOpen(false)}
                className="flex h-11 w-11 items-center justify-center rounded-full transition active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                aria-label="返回"
              >
                <ArrowLeft className="h-5 w-5" />
              </button>
              <span className="text-base font-semibold">卡包与守护</span>
            </div>
            <div className="flex-1 overflow-y-auto pt-4">
              <WalletPage
                openAgent={(p) => {
                  setWalletOpen(false);
                  openAgent(p);
                }}
              />
            </div>
          </div>
        )}

        {/* 底部导航 */}
        <nav className="fixed inset-x-0 bottom-0 z-40 mx-auto w-full max-w-md border-t bg-card/95 backdrop-blur safe-bottom lg:absolute">
          <div className="grid grid-cols-5">
            {TABS.map(({ key, label, icon: Icon }) => {
              const active = tab === key;
              if (key === "agent") {
                return (
                  <button
                    key={key}
                    onClick={() => openAgent()}
                    className="group relative flex h-16 flex-col items-center justify-center gap-0.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
                    aria-label="小犀智能助理"
                  >
                    <span
                      className={`flex h-12 w-12 -translate-y-3 items-center justify-center rounded-full shadow-lg transition-transform duration-150 active:scale-95 ${
                        active ? "bg-gold text-navy" : "hero-navy ring-pulse text-gold-light"
                      }`}
                    >
                      <Icon className="h-5 w-5" />
                    </span>
                    <span className={`absolute bottom-1.5 text-[11px] ${active ? "font-semibold text-gold" : "text-muted-foreground"}`}>
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
                    active ? "text-navy" : "text-muted-foreground"
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
