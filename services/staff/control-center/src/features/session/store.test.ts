import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const api = vi.hoisted(() => ({
  createOwnerSession: vi.fn(),
  deleteOwnerSession: vi.fn(() => Promise.resolve({ data: undefined })),
  getBootstrapState: vi.fn(() => Promise.resolve({ data: {}, etag: '"7"' })),
  renewOwnerSession: vi.fn((options?: { signal?: AbortSignal }) => {
    void options;
    return Promise.resolve({ data: undefined });
  }),
}));
const oidc = vi.hoisted(() => ({
  removeUser: vi.fn(() => Promise.resolve()),
  signinRedirect: vi.fn((args?: unknown) => {
    void args;
    return Promise.resolve();
  }),
  signinRedirectCallback: vi.fn(() =>
    Promise.resolve({
      access_token: "owner-access-token",
      state: undefined as unknown,
    }),
  ),
}));
const mutation = vi.hoisted(() => ({
  idempotencyKey: vi.fn(() => "00000000-0000-4000-8000-000000000000"),
}));
const oidcManagerSettings = vi.hoisted(() => vi.fn());

vi.mock("oidc-client-ts", () => ({
  InMemoryWebStorage: class {
    readonly kind = "memory";
  },
  UserManager: class {
    constructor(settings: unknown) {
      oidcManagerSettings(settings);
    }

    removeUser() {
      return oidc.removeUser();
    }

    signinRedirect(args?: unknown) {
      return oidc.signinRedirect(args);
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
    requestTimeoutMs: 10_000,
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

function requestBody(call: unknown[]): unknown {
  const options = call[0];
  if (typeof options !== "object" || options === null || !("body" in options))
    return undefined;
  return options.body;
}

describe("session renewal lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    api.getBootstrapState.mockReset();
    api.getBootstrapState.mockResolvedValue({ data: {}, etag: '"7"' });
    api.createOwnerSession.mockClear();
    api.createOwnerSession.mockResolvedValue({
      data: undefined,
      etag: '"8"',
    });
    api.deleteOwnerSession.mockClear();
    api.deleteOwnerSession.mockResolvedValue({ data: undefined });
    api.renewOwnerSession.mockClear();
    api.renewOwnerSession.mockResolvedValue({ data: undefined });
    oidc.removeUser.mockClear();
    oidc.signinRedirect.mockClear();
    oidc.signinRedirectCallback.mockClear();
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "owner-access-token",
      state: undefined,
    });
    mutation.idempotencyKey.mockClear();
    oidcManagerSettings.mockClear();
    const values = new Map<string, string>([["kodex.session.revision", "7"]]);
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => values.set(key, value),
    };
    vi.stubGlobal("BroadcastChannel", undefined);
    Object.defineProperty(globalThis, "window", {
      configurable: true,
      value: {
        clearTimeout: globalThis.clearTimeout,
        clearInterval: globalThis.clearInterval,
        setTimeout: globalThis.setTimeout,
        setInterval: globalThis.setInterval,
        localStorage: storage,
        sessionStorage: storage,
      },
    });
    setActivePinia(createPinia());
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
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

  test("ограничивает OIDC metadata и token requests общим HTTP budget", async () => {
    const session = useSessionStore();

    await session.beginLogin();

    expect(oidcManagerSettings).toHaveBeenCalledWith(
      expect.objectContaining({ requestTimeoutInSeconds: 10 }),
    );
  });
  test("не повторяет окончательный forbidden при продлении", async () => {
    api.renewOwnerSession.mockRejectedValueOnce({
      kind: "forbidden",
      retryable: false,
    });
    const session = useSessionStore();
    await session.probe();
    await vi.advanceTimersByTimeAsync(1);
    expect(session.phase).toBe("forbidden");
    await vi.advanceTimersByTimeAsync(600_000);
    expect(api.renewOwnerSession).toHaveBeenCalledOnce();
  });
  test("увеличивает интервал повторов временно недоступного renewal", async () => {
    api.renewOwnerSession
      .mockRejectedValueOnce({ kind: "unavailable", retryable: true })
      .mockRejectedValueOnce({ kind: "unavailable", retryable: true });
    const session = useSessionStore();
    await session.probe();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(4_999);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(3);
    expect(session.phase).toBe("authenticated");
  });

  test("объединяет параллельный OIDC redirect и показывает busy state", async () => {
    let finishRedirect!: () => void;
    oidc.signinRedirect.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        finishRedirect = resolve;
      }),
    );
    const session = useSessionStore();

    const first = session.beginLogin();
    const second = session.beginLogin();

    expect(session.phase).toBe("checking");
    expect(session.loginFailed).toBe(false);
    expect(oidc.signinRedirect).toHaveBeenCalledOnce();
    finishRedirect();
    await expect(Promise.all([first, second])).resolves.toEqual([
      undefined,
      undefined,
    ]);
  });

  test("публикует OIDC redirect failure через session error state", async () => {
    const failure = Object.assign(new TypeError("Failed to fetch"), {
      kind: "unavailable",
      retryable: true,
    });
    oidc.signinRedirect.mockRejectedValueOnce(failure);
    const session = useSessionStore();

    await expect(session.beginLogin()).rejects.toBe(failure);

    expect(session.phase).toBe("error");
    expect(session.problem).toBe(failure);
    expect(session.loginFailed).toBe(true);
  });

  test("повторяет retryable session probe и сохраняет авторизацию", async () => {
    api.getBootstrapState
      .mockRejectedValueOnce({
        kind: "unavailable",
        retryable: true,
      })
      .mockResolvedValueOnce({ data: {}, etag: '"7"' });
    const session = useSessionStore();

    const probing = session.probe();
    await vi.advanceTimersByTimeAsync(250);
    await probing;

    expect(api.getBootstrapState).toHaveBeenCalledTimes(2);
    expect(session.phase).toBe("authenticated");
  });

  test("берёт server revision из bootstrap для новой вкладки и logout", async () => {
    window.sessionStorage.removeItem("kodex.session.revision");
    api.getBootstrapState.mockResolvedValueOnce({ data: {}, etag: '"11"' });
    const session = useSessionStore();

    await session.probe();
    await session.logout();

    expect(session.canLogout).toBe(false);
    expect(requestHeaders(api.deleteOwnerSession.mock.calls[0] ?? [])).toEqual(
      expect.objectContaining({ "If-Match": '"11"' }),
    );
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
    expect(
      requestBody(api.createOwnerSession.mock.calls[0] ?? []),
    ).toBeUndefined();
    expect(session.phase).toBe("authenticated");
    expect(oidc.removeUser).toHaveBeenCalledOnce();
  });

  test("объединяет параллельную обработку одного OIDC callback", async () => {
    const session = useSessionStore();

    const first = session.completeLogin();
    const second = session.completeLogin();

    await expect(Promise.all([first, second])).resolves.toEqual([
      { kind: "login" },
      { kind: "login" },
    ]);
    expect(oidc.signinRedirectCallback).toHaveBeenCalledOnce();
    expect(api.createOwnerSession).toHaveBeenCalledOnce();
    expect(oidc.removeUser).toHaveBeenCalledOnce();
  });

  test("запрашивает fresh OIDC login с max_age=0 и operation state", async () => {
    const session = useSessionStore();

    await session.beginRuntimeSecretRevealReauth({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });

    expect(oidc.signinRedirect).toHaveBeenCalledOnce();
    const redirect = oidc.signinRedirect.mock.calls[0]?.[0] as {
      max_age?: unknown;
      prompt?: unknown;
      state?: unknown;
    };
    expect(redirect.max_age).toBe(0);
    expect(redirect.prompt).toBe("login");
    expect(redirect.state).toMatchObject({
      action: "reveal",
      kind: "runtime-secret",
      projectRef: "project_sales",
      returnPath: "/projects/project_sales/secrets",
      secretRef: "secret_main",
    });
  });
  test("выдаёт только receipt-bound email purpose после fresh OIDC и локально расходует один раз", async () => {
    const session = useSessionStore();
    const input = {
      receiptRef: "receipt_synthetic",
      receiptVersion: 7,
      receiptDigest: "a".repeat(64),
      connectionRef: "connection_synthetic",
      invocationRef: "invocation_synthetic",
    };
    await session.beginEmailReconciliationReauth(input);
    const redirect = oidc.signinRedirect.mock.calls[0]?.[0] as {
      state?: unknown;
    };
    expect(oidc.signinRedirect).toHaveBeenCalledWith(
      expect.objectContaining({ max_age: 0, prompt: "login" }),
    );
    expect(session.hasPendingEmailConfirmation(input)).toBe(false);
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "synthetic-fresh-token",
      state: redirect.state,
    });
    expect(await session.completeLogin()).toMatchObject({
      kind: "email-reconciliation",
      returnPath:
        "/integrations?connectionRef=connection_synthetic&invocationRef=invocation_synthetic",
    });
    expect(requestBody(api.createOwnerSession.mock.calls[0] ?? [])).toEqual({
      purpose: {
        kind: "EMAIL_EFFECT_RECONCILIATION",
        receiptRef: input.receiptRef,
        receiptVersion: 7,
        receiptDigest: input.receiptDigest,
      },
    });
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
    expect(
      session.hasPendingEmailConfirmation({ ...input, receiptVersion: 8 }),
    ).toBe(false);
    expect(
      session.hasPendingEmailConfirmation(input, Date.now() + 120000),
    ).toBe(false);
    expect(session.consumePendingEmailConfirmation(input)).toBe(true);
    expect(session.consumePendingEmailConfirmation(input)).toBe(false);
    session.finishEmailConfirmation();
    await vi.advanceTimersByTimeAsync(0);
    expect(api.renewOwnerSession).toHaveBeenCalledOnce();
    expect(oidc.removeUser).toHaveBeenCalledOnce();
  });

  test("после callback создаёт свежую owner session и возвращает к секретам", async () => {
    const session = useSessionStore();
    await session.beginRuntimeSecretRevealReauth({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });
    const redirect = oidc.signinRedirect.mock.calls[0]?.[0] as {
      state?: unknown;
    };
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "fresh-owner-access-token",
      state: redirect.state,
    });

    const completion = await session.completeLogin();

    expect(completion).toEqual({
      kind: "runtime-secret",
      returnPath: "/projects/project_sales/secrets",
    });
    expect(api.createOwnerSession).toHaveBeenCalledOnce();
    expect(requestBody(api.createOwnerSession.mock.calls[0] ?? [])).toEqual({
      purpose: {
        kind: "RUNTIME_SECRET_REVEAL",
        projectRef: "project_sales",
        secretRef: "secret_main",
      },
    });
    expect(oidc.removeUser).toHaveBeenCalledOnce();
    expect(
      session.hasPendingRuntimeSecretReveal("project_sales", "secret_main"),
    ).toBe(true);
    expect(
      session.consumePendingRuntimeSecretReveal("project_sales", "secret_main"),
    ).toBe(true);
    expect(
      session.consumePendingRuntimeSecretReveal("project_sales", "secret_main"),
    ).toBe(false);
  });

  test("для политики окружения создаёт обычную owner session без secret elevation", async () => {
    const session = useSessionStore();
    await session.beginRuntimeEnvironmentPolicyReauth({
      environmentRef: "environment_main",
      operation: "PUBLISH",
      projectRef: "project_sales",
    });
    const redirect = oidc.signinRedirect.mock.calls[0]?.[0] as {
      max_age?: unknown;
      prompt?: unknown;
      state?: unknown;
    };
    expect(redirect).toMatchObject({ max_age: 0, prompt: "login" });
    expect(redirect.state).toMatchObject({
      environmentRef: "environment_main",
      kind: "runtime-environment-policy",
      operation: "PUBLISH",
      projectRef: "project_sales",
      returnPath: "/projects/project_sales/environments/environment_main",
    });
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "fresh-owner-access-token",
      state: redirect.state,
    });

    await expect(session.completeLogin()).resolves.toEqual({
      kind: "runtime-environment-policy",
      returnPath: "/projects/project_sales/environments/environment_main",
    });
    expect(
      requestBody(api.createOwnerSession.mock.calls[0] ?? []),
    ).toBeUndefined();
    expect(
      session.hasPendingRuntimeSecretReveal(
        "project_sales",
        "environment_main",
      ),
    ).toBe(false);
  });

  test("отклоняет подменённый return path до создания owner session", async () => {
    const session = useSessionStore();
    await session.beginRuntimeSecretRevealReauth({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });
    const redirect = oidc.signinRedirect.mock.calls[0]?.[0] as {
      state: Record<string, unknown>;
    };
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "fresh-owner-access-token",
      state: {
        ...redirect.state,
        returnPath: "https://attacker.example/collect",
      },
    });

    await expect(session.completeLogin()).rejects.toThrow("state is invalid");
    expect(api.createOwnerSession).not.toHaveBeenCalled();
    expect(session.phase).toBe("error");
  });

  test("отклоняет повторный callback после потребления operation state", async () => {
    const session = useSessionStore();
    await session.beginRuntimeSecretRevealReauth({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });
    const redirect = oidc.signinRedirect.mock.calls[0]?.[0] as {
      state?: unknown;
    };
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "fresh-owner-access-token",
      state: redirect.state,
    });

    await session.completeLogin();
    oidc.signinRedirectCallback.mockResolvedValue({
      access_token: "replayed-owner-access-token",
      state: redirect.state,
    });
    await expect(session.completeLogin()).rejects.toThrow(
      "missing or already consumed",
    );
    expect(api.createOwnerSession).toHaveBeenCalledOnce();
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
