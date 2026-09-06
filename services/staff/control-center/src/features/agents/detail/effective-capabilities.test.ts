import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AgentEffectiveCapability,
  AgentEffectiveCapabilityPage,
} from "@/shared/api/generated/openapi/types.gen";
const api = vi.hoisted(() => ({ getAgentEffectiveCapabilities: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/problem", async (original) => ({
  ...(await original<typeof import("@/shared/api/problem")>()),
  unwrap: (value: unknown) => Promise.resolve(value),
}));
import {
  canChangePlatformCapability,
  effectiveCapabilityIdentity,
  loadEffectiveCapabilities,
} from "./effective-capabilities";

const capability: AgentEffectiveCapability = {
  key: "platform.artifact.manage",
  name: "Files",
  description: "Manage files",
  source: "PLATFORM",
  reason: "ACTOR_PERMISSION_REQUIRED",
  requested: true,
  required: false,
  effective: false,
  grantable: false,
};
function page(items = [capability]): AgentEffectiveCapabilityPage {
  return {
    agentRef: "agent-one",
    agentVersion: 7,
    projectRef: "project-one",
    runtimeConfigurationRef: "runtime-one",
    runtimeConfigurationVersion: 3,
    environmentVersionRef: "environment-version-one",
    digest: "a".repeat(64),
    evaluatedAt: "2026-09-05T00:00:00Z",
    runtimeReady: true,
    items,
    total: 43,
    nextPageToken: "next",
  };
}
const scope = {
  agentRef: "agent-one",
  agentVersion: 7,
  projectRef: "project-one",
};
beforeEach(() => {
  vi.clearAllMocks();
  api.getAgentEffectiveCapabilities.mockResolvedValue({ data: page() });
});
describe("авторитетные возможности сотрудника", () => {
  it("передаёт server query/cursor и сохраняет distinct connection grants", async () => {
    const first: AgentEffectiveCapability = {
      ...capability,
      source: "INTEGRATION",
      connectionRef: "connection-one",
      grantRef: "grant-one",
      connectionVersion: 1,
      grantVersion: 2,
      definitionDigest: "b".repeat(64),
    };
    const second = {
      ...first,
      connectionRef: "connection-two",
      grantRef: "grant-two",
    };
    api.getAgentEffectiveCapabilities.mockResolvedValue({
      data: page([first, second]),
    });
    const signal = new AbortController().signal;
    const result = await loadEffectiveCapabilities(
      scope,
      "mail",
      "cursor",
      "a".repeat(64),
      signal,
    );
    expect(result.total).toBe(43);
    expect(result.items.map(effectiveCapabilityIdentity)).toEqual([
      "platform.artifact.manage:connection-one:grant-one",
      "platform.artifact.manage:connection-two:grant-two",
    ]);
    expect(api.getAgentEffectiveCapabilities).toHaveBeenCalledWith({
      path: { agentRef: "agent-one" },
      query: {
        query: "mail",
        pageToken: "cursor",
        pageSize: 30,
        workflowRef: undefined,
        stepKey: undefined,
      },
      signal,
    });
  });
  it.each([
    { agentRef: "foreign" },
    { projectRef: "foreign" },
    { workflowRef: "workflow-other" },
    { total: 0 },
    { items: [capability, capability] },
  ])("отклоняет чужой scope или повреждённую страницу %j", async (change) => {
    api.getAgentEffectiveCapabilities.mockResolvedValue({
      data: { ...page(), ...change },
    });
    await expect(
      loadEffectiveCapabilities(
        scope,
        "",
        undefined,
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("Invalid effective capability");
  });
  it("связывает опубликованный step и не принимает неполную пару", async () => {
    await expect(
      loadEffectiveCapabilities(
        { ...scope, workflowRef: "workflow-one" },
        "",
        undefined,
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("Incomplete");
    expect(api.getAgentEffectiveCapabilities).not.toHaveBeenCalled();
    api.getAgentEffectiveCapabilities.mockResolvedValue({
      data: {
        ...page(),
        workflowRef: "workflow-one",
        stepKey: "step-one",
        workflowVersionRef: "workflow-version-one",
      },
    });
    await expect(
      loadEffectiveCapabilities(
        { ...scope, workflowRef: "workflow-one", stepKey: "step-one" },
        "",
        undefined,
        undefined,
        new AbortController().signal,
      ),
    ).resolves.toMatchObject({ stepKey: "step-one" });
  });
  it("отклоняет stale digest/version и поздний ответ после смены context", async () => {
    await expect(
      loadEffectiveCapabilities(
        scope,
        "",
        "cursor",
        "b".repeat(64),
        new AbortController().signal,
      ),
    ).rejects.toMatchObject({ status: 412 });
    await expect(
      loadEffectiveCapabilities(
        { ...scope, agentVersion: 8 },
        "",
        undefined,
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toMatchObject({ status: 412 });
    const controller = new AbortController();
    api.getAgentEffectiveCapabilities.mockImplementation(() => {
      controller.abort();
      return { data: page() };
    });
    await expect(
      loadEffectiveCapabilities(
        scope,
        "",
        undefined,
        undefined,
        controller.signal,
      ),
    ).rejects.toThrow();
  });
  it("не выдаёт capability без grantable, но сохраняет разрешённый revoke", () => {
    expect(canChangePlatformCapability(capability, true)).toBe(true);
    expect(
      canChangePlatformCapability({ ...capability, requested: false }, true),
    ).toBe(false);
    expect(
      canChangePlatformCapability(
        { ...capability, requested: false, grantable: true },
        true,
      ),
    ).toBe(true);
    expect(
      canChangePlatformCapability(
        { ...capability, source: "INTEGRATION" },
        true,
      ),
    ).toBe(false);
    expect(canChangePlatformCapability(capability, false)).toBe(false);
  });
});
