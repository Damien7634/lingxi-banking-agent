export type AuthUser = { id: string | number; username: string; displayName: string };

export class AuthError extends Error {
  status: number;
  code?: string;
  constructor(message: string, status: number, code?: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function post(url: string, body?: unknown): Promise<Response> {
  return fetch(url, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/** 返回当前用户；未登录返回 null */
export async function me(): Promise<AuthUser | null> {
  const res = await fetch("/api/auth/me", {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
  });
  if (res.status === 401) return null;
  if (!res.ok) throw new AuthError("服务暂不可用，请稍后重试", res.status);
  const data = (await res.json()) as { user: AuthUser };
  return data.user;
}

export async function login(username: string, password: string): Promise<AuthUser> {
  const res = await post("/api/auth/login", { username, password });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new AuthError(
      (data as { error?: string }).error || "登录失败，请稍后重试",
      res.status,
      (data as { code?: string }).code,
    );
  }
  return (data as { user: AuthUser }).user;
}

export async function logout(): Promise<void> {
  try {
    await post("/api/auth/logout");
  } catch {
    // 网络失败也视为本地登出
  }
}

// 全局「未登录」事件
const bus = new EventTarget();
const UNAUTH_EVENT = "unauthorized";

export function emitUnauthorized() {
  bus.dispatchEvent(new Event(UNAUTH_EVENT));
}

export function onUnauthorized(cb: () => void): () => void {
  bus.addEventListener(UNAUTH_EVENT, cb);
  return () => bus.removeEventListener(UNAUTH_EVENT, cb);
}
