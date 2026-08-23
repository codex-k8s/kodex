import type { Router, RouteLocationNormalized } from "vue-router";

import type { SessionPhase } from "@/features/session/store";

interface SessionBoundary {
  readonly phase: SessionPhase;
  probe(): Promise<void>;
}

export function requiresSessionProbe(
  route: Pick<RouteLocationNormalized, "meta">,
  phase: SessionPhase,
): boolean {
  return route.meta.public !== true && phase === "checking";
}

export function installSessionGuard(
  router: Router,
  session: SessionBoundary,
): void {
  router.beforeEach(async (route) => {
    if (requiresSessionProbe(route, session.phase)) await session.probe();
    return true;
  });
}
