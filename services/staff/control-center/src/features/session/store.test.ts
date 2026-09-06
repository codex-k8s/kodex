import { createPinia, disposePinia, setActivePinia, type Pinia } from "pinia";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import type { OwnerSessionMetadata } from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({
  beginOwnerAuthorization: vi.fn(),
  completeOwnerAuthorization: vi.fn(),
  getOwnerSession: vi.fn(),
  getBootstrapState: vi.fn(),
  deleteOwnerSession: vi.fn(),
  renewOwnerSession: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  csrfToken: () => "c".repeat(43),
  etag: (version: number) => `"${String(version)}"`,
  idempotencyKey: () => "00000000-0000-4000-8000-000000000000",
}));
vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({
    oidc: { authority: "https://identity.example.test/realms/kodex" },
  }),
}));
vi.mock("@/shared/api/problem", () => ({
  asProblem: (error: unknown) => error,
  resetUnauthorizedNotification: vi.fn(),
  unwrap: async (request: Promise<unknown>) => await request,
}));
import { useSessionStore } from "./store";

const state = "a".repeat(43);
const authorizationUrl = `https://identity.example.test/auth?state=${state}`;
const intentKey = "kodex.oidc.reauth-intent";
let version = 1;
let pinia: Pinia;
let values: Map<string, string>;
const assign = vi.fn();

function metadata(
  overrides: Partial<OwnerSessionMetadata> = {},
): OwnerSessionMetadata {
  const now = Date.now();
  return {
    generation: "11111111-1111-4111-8111-111111111111",
    version,
    sessionRevision: 7,
    serverTime: new Date(now).toISOString(),
    expiresAt: new Date(now + 1_200_000).toISOString(),
    accessExpiresAt: new Date(now + 60_000).toISOString(),
    absoluteExpiresAt: new Date(now + 3_600_000).toISOString(),
    renewAfter: new Date(now + 10_000).toISOString(),
    renewalMode: "BACKEND_REFRESH",
    ...overrides,
  };
}
function callback(code = "one"): void {
  window.location.href = `https://control.example.test/auth/callback?code=${code}&state=${state}`;
  window.sessionStorage.setItem("kodex.session.authorization-state", state);
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-09-06T10:00:00Z"));
  vi.spyOn(Math, "random").mockReturnValue(0);
  vi.clearAllMocks();
  version = 1;
  api.beginOwnerAuthorization.mockResolvedValue({ data: { authorizationUrl } });
  api.completeOwnerAuthorization.mockImplementation(() =>
    Promise.resolve({ data: metadata() }),
  );
  api.getOwnerSession.mockImplementation(() =>
    Promise.resolve({ data: metadata() }),
  );
  api.getBootstrapState.mockResolvedValue({ data: {}, etag: '"7"' });
  api.renewOwnerSession.mockImplementation(() => {
    version++;
    return Promise.resolve({ data: metadata() });
  });
  api.deleteOwnerSession.mockResolvedValue({ data: undefined });
  values = new Map();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  };
  const location = {
    href: "https://control.example.test/auth/callback",
    pathname: "/auth/callback",
    assign,
  };
  const browser = Object.assign(new EventTarget(), {
    clearTimeout: globalThis.clearTimeout,
    setTimeout: globalThis.setTimeout,
    clearInterval: globalThis.clearInterval,
    setInterval: globalThis.setInterval,
    localStorage: storage,
    sessionStorage: storage,
    location,
    history: {
      state: null,
      replaceState: vi.fn((_data: unknown, _title: string, path: string) => {
        location.href = new URL(path, location.href).toString();
      }),
    },
  });
  vi.stubGlobal("window", browser);
  vi.stubGlobal(
    "document",
    Object.assign(new EventTarget(), { visibilityState: "visible" }),
  );
  vi.stubGlobal("BroadcastChannel", undefined);
  pinia = createPinia();
  setActivePinia(pinia);
});
afterEach(() => {
  disposePinia(pinia);
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("BFF session lifecycle", () => {
  test("probe читает metadata, ждёт server renewAfter и останавливается после invalidation", async () => {
    const session = useSessionStore();
    await session.probe();
    expect(session.phase).toBe("authenticated");
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(9_999);
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(api.renewOwnerSession).toHaveBeenCalledOnce();
    session.invalidate();
    await vi.advanceTimersByTimeAsync(600_000);
    expect(api.renewOwnerSession).toHaveBeenCalledOnce();
  });
  test("не повторяет окончательный forbidden", async () => {
    api.renewOwnerSession.mockRejectedValue({
      kind: "forbidden",
      retryable: false,
    });
    const session = useSessionStore();
    await session.probe();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(session.phase).toBe("forbidden");
    await vi.advanceTimersByTimeAsync(600_000);
    expect(api.renewOwnerSession).toHaveBeenCalledOnce();
  });
  test("ограничивает число повторов и увеличивает интервал", async () => {
    api.renewOwnerSession.mockRejectedValue({
      kind: "unavailable",
      retryable: true,
    });
    const session = useSessionStore();
    await session.probe();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(4_999);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1 + 15_000 + 60_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(5);
    expect(session.phase).toBe("error");
    await vi.advanceTimersByTimeAsync(600_000);
    expect(api.renewOwnerSession).toHaveBeenCalledTimes(5);
  });
  test("legacy bearer не объявляется backend refresh и требует входа по ceiling", async () => {
    api.getOwnerSession.mockResolvedValue({
      data: metadata({ renewalMode: "REAUTHENTICATION" }),
    });
    const session = useSessionStore();
    await session.probe();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
    expect(session.phase).toBe("unauthenticated");
  });
  test("возврат во вкладку читает generation/version без продления idle", async () => {
    const session = useSessionStore();
    await session.probe();
    const before = session.connectionIdentity;
    version = 2;
    window.dispatchEvent(new Event("pageshow"));
    await vi.advanceTimersByTimeAsync(0);
    expect(api.getOwnerSession).toHaveBeenCalledTimes(2);
    expect(session.connectionIdentity).not.toBe(before);
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
  });
  test("отклоняет несовпадение CP revision и metadata", async () => {
    api.getOwnerSession.mockResolvedValue({
      data: metadata({ sessionRevision: 8 }),
    });
    const session = useSessionStore();
    await session.probe();
    expect(session.phase).toBe("error");
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
  });
  test("повторяет временно недоступный probe", async () => {
    api.getBootstrapState.mockRejectedValueOnce({
      kind: "unavailable",
      retryable: true,
    });
    const session = useSessionStore();
    const pending = session.probe();
    await vi.advanceTimersByTimeAsync(250);
    await pending;
    expect(api.getBootstrapState).toHaveBeenCalledTimes(2);
    expect(session.phase).toBe("authenticated");
  });
  test("берёт CP revision с сервера для logout и очищает локальные recovery", async () => {
    values.set("kodex.configuration.git-source-attempts", "safe-intent");
    values.set("kodex.session.revision", "99");
    const session = useSessionStore();
    await session.probe();
    await session.logout();
    expect(api.deleteOwnerSession.mock.calls[0]?.[0]).toMatchObject({
      headers: { "If-Match": '"7"' },
    });
    expect(values.has("kodex.configuration.git-source-attempts")).toBe(false);
    expect(session.connectionIdentity).toBe("");
  });
  test("объединяет redirect и передаёт браузеру только authorization URL", async () => {
    let complete!: (value: { data: { authorizationUrl: string } }) => void;
    api.beginOwnerAuthorization.mockReturnValueOnce(
      new Promise((resolve) => {
        complete = resolve;
      }),
    );
    const session = useSessionStore();
    const first = session.beginLogin();
    const second = session.beginLogin();
    await vi.advanceTimersByTimeAsync(0);
    expect(api.beginOwnerAuthorization).toHaveBeenCalledOnce();
    expect(session.phase).toBe("checking");
    complete({ data: { authorizationUrl } });
    await Promise.all([first, second]);
    expect(assign).toHaveBeenCalledOnce();
    expect(values.get("kodex.session.authorization-state")).toBe(state);
    expect([...values.values()].join(" ")).not.toContain("access_token");
  });
  test("показывает ошибку redirect без автоматического повтора", async () => {
    const failure = { kind: "unavailable", retryable: true };
    api.beginOwnerAuthorization.mockRejectedValueOnce(failure);
    const session = useSessionStore();
    await expect(session.beginLogin()).rejects.toBe(failure);
    expect(session.phase).toBe("error");
    expect(session.loginFailed).toBe(true);
    expect(api.beginOwnerAuthorization).toHaveBeenCalledOnce();
  });
  test("объединяет callback и повторяет только тот же code/state", async () => {
    callback();
    api.completeOwnerAuthorization.mockRejectedValueOnce({
      kind: "unavailable",
      retryable: true,
    });
    const session = useSessionStore();
    const first = session.completeLogin();
    const second = session.completeLogin();
    await vi.advanceTimersByTimeAsync(250);
    expect(await Promise.all([first, second])).toEqual([
      { kind: "login" },
      { kind: "login" },
    ]);
    expect(api.completeOwnerAuthorization).toHaveBeenCalledTimes(2);
    for (const call of api.completeOwnerAuthorization.mock.calls)
      expect(call[0]).toMatchObject({ body: { code: "one", state } });
    expect(window.location.href).not.toContain("code=");
    expect(values.has("kodex.session.authorization-state")).toBe(false);
  });
  test("отклоняет чужой state и повторный callback", async () => {
    callback();
    values.set("kodex.session.authorization-state", "b".repeat(43));
    const session = useSessionStore();
    await expect(session.completeLogin()).rejects.toThrow("does not match");
    expect(api.completeOwnerAuthorization).not.toHaveBeenCalled();
    callback();
    await session.completeLogin();
    await expect(session.completeLogin()).rejects.toThrow(
      "callback is invalid",
    );
    expect(api.completeOwnerAuthorization).toHaveBeenCalledOnce();
  });
  test("связывает fresh Reveal purpose с точным секретом и расходует local intent один раз", async () => {
    const session = useSessionStore();
    await session.beginRuntimeSecretRevealReauth({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });
    expect(api.beginOwnerAuthorization).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          freshAuthentication: true,
          purpose: {
            kind: "RUNTIME_SECRET_REVEAL",
            projectRef: "project_sales",
            secretRef: "secret_main",
          },
        },
      }),
    );
    callback();
    expect(await session.completeLogin()).toEqual({
      kind: "runtime-secret",
      returnPath: "/projects/project_sales/secrets",
    });
    expect(
      session.consumePendingRuntimeSecretReveal("project_sales", "secret_main"),
    ).toBe(true);
    expect(
      session.consumePendingRuntimeSecretReveal("project_sales", "secret_main"),
    ).toBe(false);
  });
  test("сохраняет receipt-bound Email purpose и после расхода читает metadata", async () => {
    const input = {
      receiptRef: "receipt_synthetic",
      receiptVersion: 7,
      receiptDigest: "a".repeat(64),
      connectionRef: "connection_synthetic",
      invocationRef: "invocation_synthetic",
    };
    const session = useSessionStore();
    await session.beginEmailReconciliationReauth(input);
    expect(api.beginOwnerAuthorization).toHaveBeenCalledWith(
      expect.objectContaining({
        body: {
          freshAuthentication: true,
          purpose: {
            kind: "EMAIL_EFFECT_RECONCILIATION",
            receiptRef: input.receiptRef,
            receiptVersion: 7,
            receiptDigest: input.receiptDigest,
          },
        },
      }),
    );
    expect(session.hasPendingEmailConfirmation(input)).toBe(false);
    callback();
    expect(await session.completeLogin()).toMatchObject({
      kind: "email-reconciliation",
    });
    expect(
      session.hasPendingEmailConfirmation({ ...input, receiptVersion: 8 }),
    ).toBe(false);
    expect(
      session.hasPendingEmailConfirmation(input, Date.now() + 120_000),
    ).toBe(false);
    expect(session.consumePendingEmailConfirmation(input)).toBe(true);
    expect(session.consumePendingEmailConfirmation(input)).toBe(false);
    version++;
    session.finishEmailConfirmation();
    await vi.advanceTimersByTimeAsync(0);
    expect(api.getOwnerSession).toHaveBeenCalledOnce();
    expect(api.renewOwnerSession).not.toHaveBeenCalled();
  });
  test("политика окружения запрашивает fresh login без secret purpose", async () => {
    const session = useSessionStore();
    await session.beginRuntimeEnvironmentPolicyReauth({
      environmentRef: "environment_main",
      operation: "PUBLISH",
      projectRef: "project_sales",
    });
    expect(api.beginOwnerAuthorization).toHaveBeenCalledWith(
      expect.objectContaining({ body: { freshAuthentication: true } }),
    );
    callback();
    expect(await session.completeLogin()).toEqual({
      kind: "runtime-environment-policy",
      returnPath: "/projects/project_sales/environments/environment_main",
    });
    expect(
      session.hasPendingRuntimeSecretReveal(
        "project_sales",
        "environment_main",
      ),
    ).toBe(false);
  });
  test("не исполняет callback с подменённым return path", async () => {
    const session = useSessionStore();
    await session.beginRuntimeSecretRevealReauth({
      projectRef: "project_sales",
      secretRef: "secret_main",
    });
    const intent = JSON.parse(values.get(intentKey) ?? "{}") as Record<
      string,
      unknown
    >;
    values.set(
      intentKey,
      JSON.stringify({ ...intent, returnPath: "https://foreign.test" }),
    );
    callback();
    await expect(session.completeLogin()).rejects.toThrow("state is invalid");
    expect(api.completeOwnerAuthorization).not.toHaveBeenCalled();
  });
  test("отменяет renewal до logout и игнорирует поздний metadata ответ после invalidate", async () => {
    let aborted = false;
    api.renewOwnerSession.mockImplementationOnce(
      (options: { signal: AbortSignal }) =>
        new Promise((_, reject) => {
          options.signal.addEventListener("abort", () => {
            aborted = true;
            reject(new DOMException("aborted", "AbortError"));
          });
        }),
    );
    const session = useSessionStore();
    await session.probe();
    await vi.advanceTimersByTimeAsync(10_000);
    await session.logout();
    expect(aborted).toBe(true);
    expect(session.phase).toBe("unauthenticated");
    await session.probe();
    let complete!: (value: { data: OwnerSessionMetadata }) => void;
    api.getOwnerSession.mockReturnValueOnce(
      new Promise((resolve) => {
        complete = resolve;
      }),
    );
    const pending = session.refreshMetadata();
    session.invalidate();
    complete({ data: metadata({ version: 99 }) });
    await pending;
    expect(session.phase).toBe("unauthenticated");
    expect(session.connectionIdentity).toBe("");
  });
});
