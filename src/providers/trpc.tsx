import { createTRPCReact } from "@trpc/react-query";
import { httpBatchLink } from "@trpc/client";
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { emitUnauthorized } from "@/lib/auth";
import superjson from "superjson";
import type { AppRouter } from "../../api/router";
import type { ReactNode } from "react";

export const trpc = createTRPCReact<AppRouter>();

function isUnauthorized(error: unknown): boolean {
  return (error as { data?: { code?: string } } | null)?.data?.code === "UNAUTHORIZED";
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error) => {
      if (isUnauthorized(error)) emitUnauthorized();
    },
  }),
  mutationCache: new MutationCache({
    onError: (error) => {
      if (isUnauthorized(error)) emitUnauthorized();
    },
  }),
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => !isUnauthorized(error) && failureCount < 3,
    },
  },
});

/** 登出/会话过期时清空所有缓存 */
export function clearQueryCache() {
  queryClient.clear();
}
const trpcClient = trpc.createClient({
  links: [
    httpBatchLink({
      url: "/api/trpc",
      transformer: superjson,
      fetch(input, init) {
        return globalThis.fetch(input, {
          ...(init ?? {}),
          credentials: "include",
        });
      },
    }),
  ],
});

export function TRPCProvider({ children }: { children: ReactNode }) {
  return (
    <trpc.Provider client={trpcClient} queryClient={queryClient}>
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    </trpc.Provider>
  );
}
