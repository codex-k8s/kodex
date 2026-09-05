import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ProviderAccount, ProviderAccountPage } from "./model";

const api = vi.hoisted(() => ({
  authorizeProviderApiKey: vi.fn(),
  createProviderAccount: vi.fn(),
  deleteProviderApiKeyAccount: vi.fn(),
  loadProviderAccounts: vi.fn(),
  loadProviderDefinitions: vi.fn(),
  reauthorizeProviderDevice: vi.fn(),
  revokeProviderAccount: vi.fn(),
  setProviderAccountEnabled: vi.fn(),
  startDeviceAuthorization: vi.fn(),
  verifyDeviceAuthorization: vi.fn(),
}));
vi.mock("./api", () => api);

import { useProvidersStore } from "./store";

function account(overrides: Partial<ProviderAccount> = {}): ProviderAccount {
  return {
    ref: "pacc_primary",
    version: 1,
    definitionKey: "openai-codex",
    name: "Основная запись",
    externalAccountMasked: "ow***er",
    state: "AUTHORIZED",
    enabled: true,
    ready: true,
    nextActions: ["DISABLE", "REVOKE", "CONFIGURE_CREDENTIAL", "TEST"],
    createdAt: "2026-08-30T08:00:00Z",
    updatedAt: "2026-08-30T08:00:00Z",
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((ready) => {
    resolve = ready;
  });
  return { promise, resolve };
}

describe("providers store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    api.loadProviderDefinitions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
  });
  afterEach(() => {
    useProvidersStore().stopAllPolling();
    vi.useRealTimers();
  });

  it("не позволяет устаревшему поиску перезаписать новый", async () => {
    const first = deferred<ProviderAccountPage>();
    const second = deferred<ProviderAccountPage>();
    api.loadProviderAccounts
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const store = useProvidersStore();

    const oldLoad = store.load("old");
    const newLoad = store.load("new");
    second.resolve({
      items: [account({ name: "NEW" })],
      nextPageToken: "",
      nextActions: ["CREATE_CONNECTION"],
    });
    await newLoad;
    first.resolve({
      items: [account({ name: "OLD" })],
      nextPageToken: "",
      nextActions: [],
    });
    await oldLoad;

    expect(store.accounts.map((item) => item.name)).toEqual(["NEW"]);
    expect(store.pageNextActions).toEqual(["CREATE_CONNECTION"]);
  });

  it("добавляет cursor-страницу без дублей", async () => {
    api.loadProviderAccounts
      .mockResolvedValueOnce({
        items: [account()],
        nextPageToken: "next",
        nextActions: [],
      })
      .mockResolvedValueOnce({
        items: [
          account({ version: 2 }),
          account({ ref: "pacc_second", name: "Вторая" }),
        ],
        nextPageToken: "",
        nextActions: [],
      });
    const store = useProvidersStore();

    await store.load();
    await store.loadMore();

    expect(store.accounts).toHaveLength(2);
    expect(
      store.accounts.find((item) => item.ref === "pacc_primary")?.version,
    ).toBe(2);
    expect(api.loadProviderAccounts).toHaveBeenLastCalledWith("", "next");
  });

  it("добавляет cursor-страницу definitions без дублей", async () => {
    api.loadProviderAccounts.mockResolvedValue({
      items: [],
      nextPageToken: "",
      nextActions: [],
    });
    api.loadProviderDefinitions
      .mockResolvedValueOnce({
        items: [
          {
            key: "openai-codex",
            name: "OpenAI Codex",
            description: "Provider",
            authorizationMethods: ["DEVICE_CODE"],
            modelIds: ["gpt-5.6"],
            defaultModelId: "gpt-5.6",
            available: true,
            ready: true,
            readinessBlockers: [],
          },
        ],
        nextPageToken: "definitions-next",
      })
      .mockResolvedValueOnce({
        items: [
          {
            key: "openai-codex",
            name: "OpenAI Codex updated",
            description: "Provider",
            authorizationMethods: ["DEVICE_CODE", "API_KEY"],
            modelIds: ["gpt-5.6"],
            defaultModelId: "gpt-5.6",
            available: true,
            ready: true,
            readinessBlockers: [],
          },
        ],
        nextPageToken: "",
      });
    const store = useProvidersStore();

    await store.load();
    await store.loadMoreDefinitions();

    expect(store.definitions).toHaveLength(1);
    expect(store.definitions[0]?.name).toBe("OpenAI Codex updated");
    expect(api.loadProviderDefinitions).toHaveBeenLastCalledWith(
      "",
      "definitions-next",
    );
  });

  it("закрыто отклоняет создание без server-owned nextAction", async () => {
    const store = useProvidersStore();

    await expect(
      store.create({ definitionKey: "openai-codex", name: "Запись" }),
    ).rejects.toThrow("Provider account creation is unavailable");
    expect(api.createProviderAccount).not.toHaveBeenCalled();

    store.pageNextActions = ["CREATE_CONNECTION"];
    api.createProviderAccount.mockResolvedValue(account());
    await store.create({ definitionKey: "openai-codex", name: "Запись" });
    expect(api.createProviderAccount).toHaveBeenCalledOnce();
  });

  it("не сохраняет API key в Pinia state и закрыто отклоняет отсутствующее действие", async () => {
    const allowed = account();
    api.authorizeProviderApiKey.mockResolvedValue(allowed);
    const store = useProvidersStore();
    store.accounts = [allowed];

    await store.authorizeApiKey(allowed, "must-never-enter-store");
    await store.authorizeApiKey(
      account({ nextActions: [] }),
      "must-not-be-sent",
    );

    expect(api.authorizeProviderApiKey).toHaveBeenCalledTimes(1);
    expect(JSON.stringify(store.$state)).not.toContain(
      "must-never-enter-store",
    );
    expect(JSON.stringify(store.$state)).not.toContain("must-not-be-sent");
  });

  it("polling вызывает explicit refresh и завершается на authorized", async () => {
    vi.useFakeTimers();
    const pending = account({
      state: "PENDING_AUTHORIZATION",
      nextActions: ["REFRESH_AUTHORIZATION", "REVOKE"],
      authorization: {
        ref: "pauth_one",
        method: "DEVICE_CODE",
        state: "PENDING",
        expiresAt: "2099-08-30T08:10:00Z",
      },
    });
    const authorized = account({ version: 2 });
    api.startDeviceAuthorization.mockResolvedValue(pending);
    api.verifyDeviceAuthorization.mockResolvedValue(authorized);
    const store = useProvidersStore();
    const configurable = account();
    store.accounts = [configurable];

    await store.startDevice(configurable);
    expect(store.pollingRefs).toEqual([pending.ref]);
    await vi.advanceTimersByTimeAsync(4_000);

    expect(api.verifyDeviceAuthorization).toHaveBeenCalledWith(pending);
    expect(store.pollingRefs).toEqual([]);
    expect(store.accounts[0]?.state).toBe("AUTHORIZED");
  });

  it("использует reauthorize для expired device flow и delete для API key", async () => {
    const expired = account({
      state: "REAUTHORIZATION_REQUIRED",
      nextActions: ["CONFIGURE_CREDENTIAL", "REVOKE"],
      authorization: {
        ref: "pauth_old",
        method: "DEVICE_CODE",
        state: "FAILED",
      },
    });
    const pending = account({
      version: 2,
      state: "PENDING_AUTHORIZATION",
      nextActions: ["REFRESH_AUTHORIZATION", "REVOKE"],
      authorization: {
        ref: "pauth_new",
        method: "DEVICE_CODE",
        state: "PENDING",
      },
    });
    api.reauthorizeProviderDevice.mockResolvedValue(pending);
    const apiKey = account({
      nextActions: ["DELETE", "REVOKE"],
      authorization: {
        ref: "pauth_key",
        method: "API_KEY",
        state: "AUTHORIZED",
      },
    });
    api.deleteProviderApiKeyAccount.mockResolvedValue({
      ...apiKey,
      state: "REVOKED",
    });
    const store = useProvidersStore();

    await store.startDevice(expired);
    await store.remove(apiKey);

    expect(api.reauthorizeProviderDevice).toHaveBeenCalledWith(expired);
    expect(api.startDeviceAuthorization).not.toHaveBeenCalled();
    expect(api.deleteProviderApiKeyAccount).toHaveBeenCalledWith(apiKey);
    expect(api.revokeProviderAccount).not.toHaveBeenCalled();
  });
  it("не подменяет отзыв API-key командой удаления", async () => {
    const item = account({
      nextActions: ["REVOKE"],
      authorization: {
        ref: "pauth_key",
        method: "API_KEY",
        state: "AUTHORIZED",
      },
    });
    api.revokeProviderAccount.mockResolvedValue({ ...item, state: "REVOKED" });
    const store = useProvidersStore();
    await store.revoke(item);
    expect(api.revokeProviderAccount).toHaveBeenCalledWith(item);
    expect(api.deleteProviderApiKeyAccount).not.toHaveBeenCalled();
    await store.remove(item);
    expect(api.deleteProviderApiKeyAccount).not.toHaveBeenCalled();
  });

  it.each(["start", "refresh"])(
    "не возобновляет polling после закрытия во время %s",
    async (operation) => {
      vi.useFakeTimers();
      const pending = account({
        state: "PENDING_AUTHORIZATION",
        nextActions: ["REFRESH_AUTHORIZATION", "CONFIGURE_CREDENTIAL"],
        authorization: {
          ref: "pauth_pending",
          method: "DEVICE_CODE",
          state: "PENDING",
          expiresAt: "2099-08-30T08:10:00Z",
        },
      });
      const response = deferred<ProviderAccount>();
      const store = useProvidersStore();
      store.accounts = [pending];
      api.startDeviceAuthorization.mockReturnValue(response.promise);
      api.verifyDeviceAuthorization.mockReturnValue(response.promise);
      const result =
        operation === "start"
          ? store.startDevice(pending)
          : store.refreshAuthorization(pending);
      store.stopAllPolling();
      response.resolve({ ...pending, version: 2 });
      await result;
      await vi.advanceTimersByTimeAsync(12_000);
      expect(store.pollingRefs).toEqual([]);
      expect(
        api.startDeviceAuthorization.mock.calls.length +
          api.verifyDeviceAuthorization.mock.calls.length,
      ).toBe(1);
    },
  );

  it("не отправляет параллельную мутацию одной учётной записи", async () => {
    const response = deferred<ProviderAccount>();
    const current = account();
    api.setProviderAccountEnabled.mockReturnValue(response.promise);
    const store = useProvidersStore();
    const first = store.setEnabled(current, false);
    await expect(store.setEnabled(current, false)).rejects.toThrow(
      "Provider account mutation is already in progress",
    );
    response.resolve({ ...current, enabled: false, version: 2 });
    await first;
    expect(api.setProviderAccountEnabled).toHaveBeenCalledOnce();
  });
});
