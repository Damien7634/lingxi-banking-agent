import { useState } from "react";
import { Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { login, type AuthUser } from "@/lib/auth";

export default function LoginPage({ onLoggedIn }: { onLoggedIn: (user: AuthUser) => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      const user = await login(username.trim(), password);
      setPassword("");
      onLoggedIn(user);
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败，请稍后重试");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="min-h-dvh bg-background">
      <div className="mx-auto flex min-h-dvh w-full max-w-md flex-col bg-background lg:my-6 lg:min-h-[calc(100dvh-3rem)] lg:overflow-hidden lg:rounded-[28px] lg:shadow-[0_24px_60px_-24px_rgba(10,37,64,0.35)]">
        <section className="hero-navy px-6 pb-10 pt-16">
          <div className="kicker text-gold-light/80">LINGXI GLOBAL</div>
          <h1 className="mt-2 text-3xl font-bold tracking-wide text-gold-light">灵犀银行</h1>
          <p className="mt-2 text-sm text-gold-light/70">跨境金融 · 智能助理</p>
        </section>

        <form onSubmit={submit} className="flex flex-1 flex-col gap-5 px-6 pt-8">
          <div className="space-y-2">
            <Label htmlFor="username">用户名</Label>
            <Input
              id="username"
              name="username"
              autoComplete="username"
              autoCapitalize="none"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              disabled={submitting}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">密码</Label>
            <Input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={submitting}
              required
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <Button
            type="submit"
            disabled={submitting || !username || !password}
            className="h-11 w-full bg-navy text-gold-light hover:bg-navy-light"
          >
            {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
            {submitting ? "登录中…" : "登录"}
          </Button>
        </form>
      </div>
    </div>
  );
}
