import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const api = vi.hoisted(() => ({
  deleteOwnerSession: vi.fn(() => Promise.resolve({ data: undefined })),
  getBootstrapState: vi.fn(() => Promise.resolve({ data: {} })),
  renewOwnerSession: vi.fn((options?: { signal?: AbortSignal }) => {
    void options;
    return Promise.resolve({ data: undefined });
  }),
}));

vi.mock("@/shared/api/client", () => ({
  requestSignal: () => AbortSignal.timeout(1_000),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  createOwnerSession: vi.fn(),
  deleteOwnerSession: api.deleteOwnerSession,
  getBootstrapState: api.getBootstrapState,
  renewOwnerSession: api.renewOwnerSession,
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "c".repeat(43),
  etag: (version: number) => `"${String(version)}"`,
  idempotencyKey: () => "00000000-0000-4000-8000-000000000000",
}));
vi.mock("@/shared/api/problem", () => ({
  asProblem: (error: unknown) => error,
  resetUnauthorizedNotification: vi.fn(),
  unwrap: async (request: Promise<unknown>) => await request,
}));

import { useSessionStore } from "./store";

describe("session renewal lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    api.getBootstrapState.mockClear();
    api.deleteOwnerSession.mockClear();
    api.deleteOwnerSession.mockResolvedValue({ data: undefined });
    api.renewOwnerSession.mockClear();
    api.renewOwnerSession.mockResolvedValue({ data: undefined });
    const values = new Map<string, string>([["kodex.session.revision", "7"]]);
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        clearInterval: globalThis.clearInterval,
        setInterval: globalThis.setInterval,
        sessionStorage: {
          getItem: (key: string) => values.get(key) ?? null,
          removeItem: (key: string) => values.delete(key),
          setItem: (key: string, value: string) => values.set(key, value),
        },
      },
    });
    setActivePinia(createPinia());
  });

  afterEach(() => {
    vi.useRealTimers();
    Reflect.deleteProperty(globalThis, "window");
  });

  test("продлевает session после probe и останавливается после invalidation", async () => {
    const session = useSessionStore();
    await session.probe();
    vi.runAllTicks();

    expect(session.phase).toBe("authenticated");
    expect(api.renewOwnerSession).toHaveBeenCalledOnce();

    await vi.advanceTimersByTimeAsync(5 * 60 * 1_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(2);

    session.invalidate();
    await vi.advanceTimersByTimeAsync(10 * 60 * 1_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(2);
  });

  test("отменяет renewal и ждёт его завершения перед logout", async () => {
    let renewalAborted = false;
    api.renewOwnerSession.mockImplementationOnce(
      (options?: { signal?: AbortSignal }) =>
        new Promise((_, reject) => {
          options?.signal?.addEventListener(
            "abort",
            () => {
              renewalAborted = true;
              reject(new DOMException("aborted", "AbortError"));
            },
            { once: true },
          );
        }),
    );
    api.deleteOwnerSession.mockImplementationOnce(() => {
      expect(renewalAborted).toBe(true);
      return Promise.resolve({ data: undefined });
    });

    const session = useSessionStore();
    await session.probe();
    vi.runAllTicks();
    await session.logout();

    expect(api.deleteOwnerSession).toHaveBeenCalledOnce();
    expect(session.phase).toBe("unauthenticated");
  });
});
