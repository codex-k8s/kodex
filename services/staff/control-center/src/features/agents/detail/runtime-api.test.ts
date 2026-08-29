import { beforeEach, describe, expect, it, vi } from "vitest";

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
  loadRuntimeCatalog,
  saveOverlayDraft,
  searchRuntimeEnvironments,
} from "@/features/agents/detail/runtime-api";

describe("agent detail runtime api", () => {
  beforeEach(() => vi.clearAllMocks());

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
});
