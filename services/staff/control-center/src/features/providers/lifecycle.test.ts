import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ProviderAccount,
  ProviderAccountBlockerPage,
  ProviderAccountQueuedWorkCancellation,
  ProviderAccountQueuedWorkCancellationInput,
} from "@/shared/api/generated/openapi/types.gen";
import { KnownMutationRejection } from "@/shared/api/mutation-rejection";
import { AppProblem } from "@/shared/api/problem";
const api = vi.hoisted(() => ({
  loadProviderAccount: vi.fn(),
  deleteProviderAccountRecord:
    vi.fn<
      (account: ProviderAccount, key?: string) => Promise<ProviderAccount>
    >(),
  verifyDeviceAuthorization:
    vi.fn<
      (account: ProviderAccount, key?: string) => Promise<ProviderAccount>
    >(),
  reauthorizeProviderDevice:
    vi.fn<
      (account: ProviderAccount, key?: string) => Promise<ProviderAccount>
    >(),
}));
vi.mock("./api", () => api);
const sdk = vi.hoisted(() => ({
  cancelProviderAccountQueuedWork: vi.fn<
    (options: {
      body: ProviderAccountQueuedWorkCancellationInput;
      headers: Record<string, string>;
    }) => Promise<{
      data?: ProviderAccountQueuedWorkCancellation;
      error?: unknown;
      response: Response;
    }>
  >(),
  listProviderAccountBlockers:
    vi.fn<
      () => Promise<{ data: ProviderAccountBlockerPage; response: Response }>
    >(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/shared/api/mutation")>()),
  csrfToken: () => "synthetic-csrf",
}));
import {
  checkedProviderBlockerPage,
  retryProviderLifecycle,
  startProviderLifecycle,
} from "./lifecycle";
import {
  readProviderLifecycleAttempt,
  rememberProviderLifecycleAttempt,
  type ProviderLifecycleAttempt,
} from "./lifecycle-attempt";
const account: ProviderAccount = {
  ref: "pacc_synthetic",
  version: 9,
  definitionKey: "openai-codex",
  name: "Проверка",
  externalAccountMasked: "",
  state: "DELETING",
  enabled: false,
  ready: false,
  nextActions: [],
  createdAt: "2026-09-01T00:00:00Z",
  updatedAt: "2026-09-01T00:00:00Z",
};
const attempt: ProviderLifecycleAttempt = {
  accountRef: account.ref,
  version: 3,
  key: "11111111-1111-4111-8111-111111111111",
  action: "DELETE",
};
const failedAccount: ProviderAccount = {
  ...account,
  nextActions: ["DELETE"],
  deletion: {
    ref: "pdel_synthetic",
    version: 4,
    state: "FAILED",
    pendingCleanup: 1,
    requestedAt: "2026-09-01T00:00:00Z",
    safeReason: "CREDENTIAL_CLEANUP_FAILED",
    blockers: [
      { kind: "AGENT", total: 0 },
      { kind: "PROVIDER_POOL", total: 0 },
      { kind: "AUTOMATION", total: 0 },
      { kind: "ACTIVE_TURN", total: 0 },
      { kind: "QUEUED_TURN", total: 0 },
      { kind: "WARM_RUNTIME", total: 0 },
    ],
  },
};
function storage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    key: (index) => [...values.keys()][index] ?? null,
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
  };
}
function page(): ProviderAccountBlockerPage {
  return {
    items: [],
    total: 2,
    hiddenCount: 4,
    contextDigest: "a".repeat(64),
    accountVersion: 9,
    deletionIntentVersion: 1,
    nextPageToken: "next",
  };
}
describe("provider lifecycle recovery", () => {
  beforeEach(() => vi.resetAllMocks());
  it.each([400, 412, 422] as const)(
    "отмена очереди после первого отказа %s получает новый key/OCC, а UNKNOWN сохраняет прежний",
    async (status) => {
      const data = storage();
      const action = {
        action: "CANCEL_QUEUED",
        body: {
          selectedRunRefs: ["run_synthetic"],
          blockersDigest: "a".repeat(64),
        },
      } as const;
      const command = {
        ...action,
        body: {
          ...action.body,
          selectedRunRefs: [...action.body.selectedRunRefs],
        },
      };
      const rejection = () => ({
        error: {
          status,
          code:
            status === 412 ? "VERSION_OR_STATE_CONFLICT" : "INVALID_REQUEST",
          retryable: status === 412,
        },
        response: new Response(null, {
          status,
          headers: { "Content-Type": "application/problem+json" },
        }),
      });
      sdk.cancelProviderAccountQueuedWork.mockResolvedValueOnce(rejection());
      await expect(
        startProviderLifecycle(
          account,
          command,
          data,
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ status });
      expect(readProviderLifecycleAttempt(account.ref, data)).toBeUndefined();
      const original = sdk.cancelProviderAccountQueuedWork.mock.calls[0]?.[0];
      sdk.cancelProviderAccountQueuedWork.mockRejectedValueOnce(
        new Error("lost ACK"),
      );
      const current = { ...account, version: 10 };
      await expect(
        startProviderLifecycle(
          current,
          command,
          data,
          new AbortController().signal,
        ),
      ).rejects.toThrow();
      const pending = readProviderLifecycleAttempt(account.ref, data);
      expect(pending).toMatchObject({ version: 10, action: "CANCEL_QUEUED" });
      const sent = sdk.cancelProviderAccountQueuedWork.mock.calls[1]?.[0];
      expect(sent?.headers["If-Match"]).toBe('"10"');
      expect(sent?.headers["Idempotency-Key"]).not.toBe(
        original?.headers["Idempotency-Key"],
      );
      if (!pending) throw new Error("Missing synthetic pending cancellation");
      api.loadProviderAccount.mockResolvedValue(current);
      sdk.listProviderAccountBlockers.mockResolvedValue({
        data: { ...page(), accountVersion: 10 },
        response: new Response(null),
      });
      sdk.cancelProviderAccountQueuedWork.mockResolvedValueOnce(rejection());
      await expect(
        retryProviderLifecycle(pending, data, new AbortController().signal),
      ).rejects.toMatchObject({ status });
      expect(readProviderLifecycleAttempt(account.ref, data)).toEqual(pending);
      expect(
        sdk.cancelProviderAccountQueuedWork.mock.calls[2]?.[0].headers,
      ).toEqual(sent?.headers);
      expect(sdk.cancelProviderAccountQueuedWork).toHaveBeenCalledTimes(3);
    },
  );
  it("новое удаление FAILED требует серверного DELETE и использует свежие key/OCC", async () => {
    const data = storage();
    const failed = failedAccount;
    api.deleteProviderAccountRecord.mockResolvedValue(failed);
    await startProviderLifecycle(
      failed,
      { action: "DELETE" },
      data,
      new AbortController().signal,
    );
    const [sent, key] = api.deleteProviderAccountRecord.mock.calls[0] ?? [];
    expect(sent).toEqual(failed);
    expect(key).toMatch(/^[a-f0-9-]{36}$/);
    expect(key).not.toBe(attempt.key);
    expect(api.deleteProviderAccountRecord).toHaveBeenCalledOnce();
    expect(readProviderLifecycleAttempt(account.ref, data)).toBeUndefined();
  });
  it("без server DELETE не создаёт новый intent и не вызывает API", async () => {
    const data = storage();
    await expect(
      startProviderLifecycle(
        { ...failedAccount, nextActions: [] },
        { action: "DELETE" },
        data,
        new AbortController().signal,
      ),
    ).rejects.toThrow("deletion is unavailable");
    expect(api.deleteProviderAccountRecord).not.toHaveBeenCalled();
    expect(readProviderLifecycleAttempt(account.ref, data)).toBeUndefined();
  });
  it.each([
    ["DELETE", api.deleteProviderAccountRecord],
    ["VERIFY", api.verifyDeviceAuthorization],
    ["REAUTHORIZE", api.reauthorizeProviderDevice],
  ] as const)(
    "первый известный %s 412 разрешает новый intent со свежим OCC",
    async (action, send) => {
      const data = storage();
      send.mockRejectedValueOnce(new KnownMutationRejection(412));
      await expect(
        startProviderLifecycle(
          failedAccount,
          { action },
          data,
          new AbortController().signal,
        ),
      ).rejects.toMatchObject({ status: 412 });
      expect(readProviderLifecycleAttempt(account.ref, data)).toBeUndefined();
      const firstKey = send.mock.calls[0]?.[1];
      const refreshed = { ...failedAccount, version: 10 };
      send.mockResolvedValueOnce(refreshed);
      await startProviderLifecycle(
        refreshed,
        { action },
        data,
        new AbortController().signal,
      );
      expect(send.mock.calls[1]?.[0]).toMatchObject({ version: 10 });
      expect(send.mock.calls[1]?.[1]).not.toBe(firstKey);
      expect(send).toHaveBeenCalledTimes(2);
    },
  );
  it.each([new KnownMutationRejection(412), new KnownMutationRejection(400)])(
    "известный отказ replay не освобождает прежний UNKNOWN: %j",
    async (error) => {
      const data = storage();
      rememberProviderLifecycleAttempt(attempt, data);
      api.loadProviderAccount.mockResolvedValue(failedAccount);
      api.deleteProviderAccountRecord.mockRejectedValueOnce(error);
      await expect(
        retryProviderLifecycle(attempt, data, new AbortController().signal),
      ).rejects.toThrow();
      expect(readProviderLifecycleAttempt(account.ref, data)).toEqual(attempt);
      await expect(
        startProviderLifecycle(
          failedAccount,
          { action: "DELETE" },
          data,
          new AbortController().signal,
        ),
      ).rejects.toThrow("cannot be replaced");
    },
  );
  it.each([400, 412, 422])(
    "первый недоказанный ответ %s сохраняет intent",
    async (status) => {
      const data = storage();
      api.deleteProviderAccountRecord.mockRejectedValueOnce(
        new AppProblem({
          status,
          code: "UNKNOWN",
          retryable: false,
          kind: "unknown",
        }),
      );
      await expect(
        startProviderLifecycle(
          failedAccount,
          { action: "DELETE" },
          data,
          new AbortController().signal,
        ),
      ).rejects.toThrow();
      expect(readProviderLifecycleAttempt(account.ref, data)).toMatchObject({
        version: failedAccount.version,
        action: "DELETE",
      });
    },
  );
  it("UNKNOWN сначала требует старый replay, даже когда новый DELETE уже разрешён", async () => {
    const data = storage();
    const failed = failedAccount;
    rememberProviderLifecycleAttempt(attempt, data);
    await expect(
      startProviderLifecycle(
        failed,
        { action: "DELETE" },
        data,
        new AbortController().signal,
      ),
    ).rejects.toThrow("cannot be replaced");
    expect(api.deleteProviderAccountRecord).not.toHaveBeenCalled();
    api.loadProviderAccount.mockResolvedValue(failed);
    api.deleteProviderAccountRecord.mockResolvedValue(account);
    await retryProviderLifecycle(attempt, data, new AbortController().signal);
    expect(api.deleteProviderAccountRecord).toHaveBeenCalledExactlyOnceWith(
      { ...failed, version: attempt.version },
      attempt.key,
    );
    expect(readProviderLifecycleAttempt(account.ref, data)).toBeUndefined();
    await startProviderLifecycle(
      failed,
      { action: "DELETE" },
      data,
      new AbortController().signal,
    );
    expect(api.deleteProviderAccountRecord).toHaveBeenCalledTimes(2);
    const [sent, key] = api.deleteProviderAccountRecord.mock.calls[1] ?? [];
    expect(sent?.version).toBe(failed.version);
    expect(key).not.toBe(attempt.key);
  });
  it("после защищённого чтения повторяет исходный OCC и ключ, не текущую версию", async () => {
    const data = storage();
    rememberProviderLifecycleAttempt(attempt, data);
    api.loadProviderAccount.mockResolvedValue(account);
    api.deleteProviderAccountRecord.mockResolvedValue({
      ...account,
      state: "DELETED",
    });
    await retryProviderLifecycle(attempt, data, new AbortController().signal);
    expect(api.loadProviderAccount).toHaveBeenCalledOnce();
    expect(api.deleteProviderAccountRecord).toHaveBeenCalledExactlyOnceWith(
      { ...account, version: 3 },
      attempt.key,
    );
    expect(readProviderLifecycleAttempt(account.ref, data)).toBeUndefined();
  });
  it("отказ защищённого чтения не повторяет команду и сохраняет неизвестный исход", async () => {
    const data = storage();
    rememberProviderLifecycleAttempt(attempt, data);
    api.loadProviderAccount.mockRejectedValue(new Error("Access denied"));
    await expect(
      retryProviderLifecycle(attempt, data, new AbortController().signal),
    ).rejects.toThrow("Access denied");
    expect(api.deleteProviderAccountRecord).not.toHaveBeenCalled();
    expect(readProviderLifecycleAttempt(account.ref, data)).toEqual(attempt);
  });
  it("не принимает чужую проекцию при восстановлении", async () => {
    const data = storage();
    rememberProviderLifecycleAttempt(attempt, data);
    api.loadProviderAccount.mockResolvedValue({
      ...account,
      ref: "pacc_foreign",
    });
    await expect(
      retryProviderLifecycle(attempt, data, new AbortController().signal),
    ).rejects.toThrow("scope changed");
    expect(api.deleteProviderAccountRecord).not.toHaveBeenCalled();
  });
  it("отмена чтения перед resend не запускает команду и не теряет intent", async () => {
    const data = storage();
    rememberProviderLifecycleAttempt(attempt, data);
    const controller = new AbortController();
    api.loadProviderAccount.mockImplementation(() => {
      controller.abort();
      return Promise.resolve(account);
    });
    await expect(
      retryProviderLifecycle(attempt, data, controller.signal),
    ).rejects.toThrow();
    expect(api.deleteProviderAccountRecord).not.toHaveBeenCalled();
    expect(readProviderLifecycleAttempt(account.ref, data)).toEqual(attempt);
  });
  it("страницы сохраняют owner digest, версию удаления и скрытое количество", () => {
    const first = page();
    expect(() =>
      checkedProviderBlockerPage(
        { ...first, nextPageToken: undefined },
        9,
        first,
      ),
    ).not.toThrow();
    for (const changed of [
      { contextDigest: "b".repeat(64) },
      { deletionIntentVersion: 2 },
      { hiddenCount: 0 },
      { accountVersion: 10 },
      { total: 1 },
    ])
      expect(() =>
        checkedProviderBlockerPage(
          { ...first, ...changed, nextPageToken: undefined },
          9,
          first,
        ),
      ).toThrow("snapshot changed");
    expect(() => checkedProviderBlockerPage(first, 9, first)).toThrow(
      "snapshot changed",
    );
  });
});
