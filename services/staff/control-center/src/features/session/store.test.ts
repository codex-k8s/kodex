import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const api = vi.hoisted(() => ({
  createOwnerSession: vi.fn(),
  deleteOwnerSession: vi.fn(() => Promise.resolve({ data: undefined })),
  getBootstrapState: vi.fn(() => Promise.resolve({ data: {} })),
  renewOwnerSession: vi.fn((options?: { signal?: AbortSignal }) => {
    void options;
    return Promise.resolve({ data: undefined });
  }),
}));
const oidc = vi.hoisted(() => ({
  removeUser: vi.fn(() => Promise.resolve()),
  signinRedirect: vi.fn(() => Promise.resolve()),
  signinRedirectCallback: vi.fn(() =>
    Promise.resolve({ access_token: "owner-access-token" }),
  ),
}));
const mutation = vi.hoisted(() => ({
  idempotencyKey: vi.fn(() => "00000000-0000-4000-8000-000000000000"),
}));

vi.mock("oidc-client-ts", () => ({
  InMemoryWebStorage: class {
    readonly kind = "memory";
  },
  UserManager: class {
    removeUser() {
      return oidc.removeUser();
    }

    signinRedirect() {
      return oidc.signinRedirect();
    }

    signinRedirectCallback() {
      return oidc.signinRedirectCallback();
    }
  },
  WebStorageStateStore: class {
    readonly options: unknown;

    constructor(options: unknown) {
      this.options = options;
    }
  },
}));

vi.mock("@/shared/api/client", () => ({
  requestSignal: () => AbortSignal.timeout(1_000),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  createOwnerSession: api.createOwnerSession,
  deleteOwnerSession: api.deleteOwnerSession,
  getBootstrapState: api.getBootstrapState,
  renewOwnerSession: api.renewOwnerSession,
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "c".repeat(43),
  etag: (version: number) => `"${String(version)}"`,
  idempotencyKey: mutation.idempotencyKey,
}));
vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({
    apiBaseUrl: "https://control.example.test",
    oidc: {
      authority: "https://identity.example.test/realms/kodex",
      clientId: "control-center",
      postLogoutRedirectUri: "https://control.example.test/",
      redirectUri: "https://control.example.test/auth/callback",
      scope: "openid profile email",
    },
  }),
}));
vi.mock("@/shared/api/problem", () => ({
  asProblem: (error: unknown) => error,
  resetUnauthorizedNotification: vi.fn(),
  unwrap: async (request: Promise<unknown>) => await request,
}));

import { useSessionStore } from "./store";

function requestHeaders(call: unknown[]): unknown {
  const options = call[0];
  if (
    typeof options !== "object" ||
    options === null ||
    !("headers" in options)
  ) {
    return undefined;
  }
  return options.headers;
}

describe("session renewal lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    api.getBootstrapState.mockClear();
    api.createOwnerSession.mockClear();
    api.deleteOwnerSession.mockClear();
    api.deleteOwnerSession.mockResolvedValue({ data: undefined });
    api.renewOwnerSession.mockClear();
    api.renewOwnerSession.mockResolvedValue({ data: undefined });
    oidc.removeUser.mockClear();
    oidc.signinRedirect.mockClear();
    oidc.signinRedirectCallback.mockClear();
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "owner-access-token",
    });
    mutation.idempotencyKey.mockClear();
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

  test("повторяет retryable session probe и сохраняет авторизацию", async () => {
    api.getBootstrapState
      .mockRejectedValueOnce({
        kind: "unavailable",
        retryable: true,
      })
      .mockResolvedValueOnce({ data: {} });
    const session = useSessionStore();

    const probing = session.probe();
    await vi.advanceTimersByTimeAsync(250);
    await probing;

    expect(api.getBootstrapState).toHaveBeenCalledTimes(2);
    expect(session.phase).toBe("authenticated");
  });

  test("повторяет создание owner session с одним idempotency key", async () => {
    api.createOwnerSession
      .mockRejectedValueOnce({ kind: "unavailable", retryable: true })
      .mockResolvedValueOnce({ data: undefined, etag: '"8"' });
    const session = useSessionStore();

    const completing = session.completeLogin();
    await vi.advanceTimersByTimeAsync(250);
    await completing;

    expect(api.createOwnerSession).toHaveBeenCalledTimes(2);
    expect(requestHeaders(api.createOwnerSession.mock.calls[0] ?? [])).toEqual(
      requestHeaders(api.createOwnerSession.mock.calls[1] ?? []),
    );
    expect(mutation.idempotencyKey).toHaveBeenCalledOnce();
    expect(session.phase).toBe("authenticated");
    expect(oidc.removeUser).toHaveBeenCalledOnce();
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
