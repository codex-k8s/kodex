import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  bindAgentRuntimeEnvironment: vi.fn(),
  createConfigOverlayDraft: vi.fn(),
  getAgentRuntimeConfiguration: vi.fn(),
  listRuntimeEnvironmentSets: vi.fn(),
  listRuntimeSelections: vi.fn(),
  publishAgentRuntimeConfiguration: vi.fn(),
  publishConfigOverlayDraft: vi.fn(),
  validateConfigOverlayDraft: vi.fn(),
  signal: new AbortController().signal,
}));

vi.mock("@/shared/api/client", () => ({
  requestSignal: () => mocks.signal,
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => ({
  bindAgentRuntimeEnvironment: mocks.bindAgentRuntimeEnvironment,
  createConfigOverlayDraft: mocks.createConfigOverlayDraft,
  getAgentRuntimeConfiguration: mocks.getAgentRuntimeConfiguration,
  listRuntimeEnvironmentSets: mocks.listRuntimeEnvironmentSets,
  listRuntimeSelections: mocks.listRuntimeSelections,
  publishAgentRuntimeConfiguration: mocks.publishAgentRuntimeConfiguration,
  publishConfigOverlayDraft: mocks.publishConfigOverlayDraft,
  validateConfigOverlayDraft: mocks.validateConfigOverlayDraft,
}));
vi.mock("@/shared/api/problem", () => ({
  asProblem: (error: unknown) => error,
  unwrap: (value: unknown) => Promise.resolve(value),
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version?: number,
  ) =>
    request({
      "If-Match": version === undefined ? "" : String(version),
      "Idempotency-Key": "test-idempotency",
      "X-CSRF-Token": "test-csrf",
    }),
}));

import {
  loadAgentRuntime,
  loadRuntimeCatalog,
  saveAgentRuntime,
  saveOverlayDraft,
  searchRuntimeEnvironments,
} from "@/features/agents/detail/runtime-api";

describe("agent detail runtime api", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => vi.useRealTimers());

  it("читает runtime catalog и передаёт серверу cursor-поиск окружений", async () => {
    mocks.listRuntimeSelections.mockResolvedValue({
      data: {
        items: [
          {
            ref: "runtime_openai",
            name: "OpenAI Codex",
            revision: "runtime-v3",
            ready: true,
            provider: "openai-codex",
            model: "gpt-5.6-sol",
          },
        ],
      },
    });
    mocks.listRuntimeEnvironmentSets.mockResolvedValue({
      data: { items: [], nextPageToken: "environment_next" },
    });

    await expect(loadRuntimeCatalog()).resolves.toHaveLength(1);
    await expect(
      searchRuntimeEnvironments("project_sales", " docs ", "cursor-1"),
    ).resolves.toMatchObject({ nextPageToken: "environment_next" });
    expect(mocks.listRuntimeEnvironmentSets).toHaveBeenCalledWith({
      path: { projectRef: "project_sales" },
      query: { query: "docs", pageToken: "cursor-1", pageSize: 30 },
      signal: mocks.signal,
    });
  });

  it("сохраняет config.toml через versioned mutation без fake success", async () => {
    const view = { agentVersion: 8 };
    mocks.createConfigOverlayDraft.mockResolvedValue({ data: view });

    await expect(
      saveOverlayDraft("agent_sales", 'model_reasoning_effort = "xhigh"', 7),
    ).resolves.toBe(view);
    expect(mocks.createConfigOverlayDraft).toHaveBeenCalledWith({
      path: { agentRef: "agent_sales" },
      body: { content: 'model_reasoning_effort = "xhigh"' },
      headers: {
        "If-Match": "7",
        "Idempotency-Key": "test-idempotency",
        "X-CSRF-Token": "test-csrf",
      },
      signal: mocks.signal,
    });
  });

  it("после неопределённого ответа мутации принимает только совпавший авторитетный runtime", async () => {
    const uncertain = Object.assign(new Error("response body was lost"), {
      retryable: true,
    });
    const input = {
      runtimeProfileRef: "runtime_openai",
      model: "gpt-5.6-sol",
      providerPolicyMode: "FIXED" as const,
      providerAccounts: [{ accountRef: "account-2", weight: 1 }],
    };
    const authoritative = {
      agentVersion: 9,
      configuration: {
        runtimeProfileRef: input.runtimeProfileRef,
        model: input.model,
        providerPolicy: {
          mode: input.providerPolicyMode,
          accountCandidates: input.providerAccounts,
        },
      },
    };
    mocks.publishAgentRuntimeConfiguration.mockRejectedValueOnce(uncertain);
    mocks.getAgentRuntimeConfiguration.mockResolvedValueOnce({
      data: authoritative,
    });

    await expect(saveAgentRuntime("agent_sales", input, 8)).resolves.toBe(
      authoritative,
    );
    expect(mocks.publishAgentRuntimeConfiguration).toHaveBeenCalledTimes(1);
    expect(mocks.getAgentRuntimeConfiguration).toHaveBeenCalledTimes(1);
  });

  it("не скрывает неопределённый ответ мутации при несовпавшем readback", async () => {
    const uncertain = Object.assign(new Error("response body was lost"), {
      retryable: true,
    });
    mocks.publishAgentRuntimeConfiguration.mockRejectedValueOnce(uncertain);
    mocks.getAgentRuntimeConfiguration.mockResolvedValueOnce({
      data: {
        configuration: {
          runtimeProfileRef: "runtime_old",
          model: "gpt-5.5-sol",
          providerPolicy: { mode: "LEAST_USED", accountCandidates: [] },
        },
      },
    });

    await expect(
      saveAgentRuntime(
        "agent_sales",
        {
          runtimeProfileRef: "runtime_openai",
          model: "gpt-5.6-sol",
          providerPolicyMode: "FIXED",
          providerAccounts: [{ accountRef: "account-2", weight: 1 }],
        },
        8,
      ),
    ).rejects.toBe(uncertain);
    expect(mocks.publishAgentRuntimeConfiguration).toHaveBeenCalledTimes(1);
    expect(mocks.getAgentRuntimeConfiguration).toHaveBeenCalledTimes(1);
  });

  it("использует расширенный bounded retry для runtime-конфигурации", async () => {
    vi.useFakeTimers();
    const timeoutSpy = vi.spyOn(globalThis, "setTimeout");
    const transient = Object.assign(new Error("Failed to fetch"), {
      retryable: true,
    });
    const authoritative = { agentVersion: 4 };
    mocks.getAgentRuntimeConfiguration
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockRejectedValueOnce(transient)
      .mockResolvedValueOnce({ data: authoritative });

    const result = loadAgentRuntime("agent_sales");
    await vi.runAllTimersAsync();
    await expect(result).resolves.toBe(authoritative);
    expect(mocks.getAgentRuntimeConfiguration).toHaveBeenCalledTimes(5);
    expect(timeoutSpy.mock.calls.map((call) => call[1])).toEqual([
      200, 600, 1_500, 3_000,
    ]);
  });

  it("не расширяет короткий retry для runtime-каталога", async () => {
    vi.useFakeTimers();
    const transient = Object.assign(new Error("Failed to fetch"), {
      retryable: true,
    });
    mocks.listRuntimeSelections.mockRejectedValue(transient);

    const result = loadRuntimeCatalog();
    const rejected = expect(result).rejects.toBe(transient);
    await vi.runAllTimersAsync();
    await rejected;
    expect(mocks.listRuntimeSelections).toHaveBeenCalledTimes(3);
  });
});
