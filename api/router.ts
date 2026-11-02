import { createRouter, publicQuery } from "./middleware";
import { accountRouter, transferRouter } from "./routers/account";
import { billRouter } from "./routers/bill";
import { wealthRouter } from "./routers/wealth";
import { cardRouter, scenarioRouter } from "./routers/card";
import { fxRouter } from "./routers/fx";
import { scfRouter } from "./routers/scf";
import { tradeRouter } from "./routers/trade";

export const appRouter = createRouter({
  ping: publicQuery.query(() => ({ ok: true, ts: Date.now() })),
  account: accountRouter,
  transfer: transferRouter,
  bill: billRouter,
  wealth: wealthRouter,
  card: cardRouter,
  scenario: scenarioRouter,
  fx: fxRouter,
  scf: scfRouter,
  trade: tradeRouter,
});

export type AppRouter = typeof appRouter;
