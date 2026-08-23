import { describe, expect, it, vi } from "vitest";
import type { NavigationGuard, Router } from "vue-router";

import {
  installSessionGuard,
  requiresSessionProbe,
} from "@/app/router/session-guard";

describe("session route guard", () => {
  it("проверяет сессию только при первом входе на защищённый route", () => {
    expect(requiresSessionProbe({ meta: {} }, "checking")).toBe(true);
    expect(requiresSessionProbe({ meta: { public: true } }, "checking")).toBe(
      false,
    );
    expect(requiresSessionProbe({ meta: {} }, "authenticated")).toBe(false);
    expect(requiresSessionProbe({ meta: {} }, "unauthenticated")).toBe(false);
  });

  it("не пропускает начальную навигацию до завершения authoritative probe", async () => {
    let guard: NavigationGuard | undefined;
    const router = {
      beforeEach(value: NavigationGuard) {
        guard = value;
        return () => undefined;
      },
    } as unknown as Router;
    const probe = vi.fn(() => Promise.resolve());
    const session = { phase: "checking" as const, probe };

    installSessionGuard(router, session);
    await guard?.({ meta: {} } as never, {} as never, vi.fn());

    expect(probe).toHaveBeenCalledOnce();
  });
});
