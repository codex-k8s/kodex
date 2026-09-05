import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AgentContextBinding,
  AgentRuntimeConfigurationView,
} from "@/shared/api/generated/openapi/types.gen";
const client = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: async (
    request: (headers: Record<string, string>) => Promise<{ data: unknown }>,
    version: number,
  ) =>
    request({
      "If-Match": `"${String(version)}"`,
      "Idempotency-Key": "synthetic-key",
      "X-CSRF-Token": "synthetic-csrf",
    }),
}));
import {
  readBindings,
  changeBinding,
  checkBindingView,
  type ContextBindingSnapshot,
} from "./bindings";
const binding: AgentContextBinding = {
  ref: "binding",
  version: 4,
  agentRef: "agent",
  resourceRef: "skill",
  revisionRef: "revision_old",
  digest: "a".repeat(64),
};
const snapshot: ContextBindingSnapshot = {
  agentRef: "agent",
  projectRef: "project",
  agentName: "Agent",
  agentVersion: 17,
  skillBindings: [binding],
  memoryBindings: [],
};
function view(): Parameters<typeof checkBindingView>[0] {
  return {
    configuration: { agentRef: "agent" },
    agentVersion: 17,
    skillBindings: [binding],
    memoryBindings: [],
  };
}
describe("context binding OCC", () => {
  beforeEach(() => {
    client.get.mockReset();
    client.put.mockReset();
    client.delete.mockReset();
  });
  it("проверяет обязательные массивы и ETag agent, не runtime configuration version", async () => {
    client.get
      .mockResolvedValueOnce({
        response: new Response(),
        data: {
          ref: "agent",
          projectRef: "project",
          name: "Agent",
          system: false,
        },
      })
      .mockResolvedValueOnce({
        response: new Response(null, { headers: { ETag: '"17"' } }),
        data: view(),
      });
    expect(
      await readBindings("project", "agent", new AbortController().signal),
    ).toEqual(snapshot);
    expect(() =>
      checkBindingView(
        {
          ...view(),
          memoryBindings: undefined,
        } as unknown as AgentRuntimeConfigurationView,
        "agent",
      ),
    ).toThrow();
    expect(() =>
      checkBindingView(
        { ...view(), skillBindings: [binding, binding] },
        "agent",
      ),
    ).toThrow();
    expect(() =>
      checkBindingView(
        {
          ...view(),
          memoryBindings: [{ ...binding, ref: "foreign", agentRef: "other" }],
        },
        "agent",
      ),
    ).toThrow();
  });
  it("закрыто отклоняет отсутствующий либо неверный ETag", async () => {
    client.get
      .mockResolvedValueOnce({
        response: new Response(),
        data: {
          ref: "agent",
          projectRef: "project",
          name: "Agent",
          system: false,
        },
      })
      .mockResolvedValueOnce({
        response: new Response(null, { headers: { ETag: '"4"' } }),
        data: view(),
      });
    await expect(
      readBindings("project", "agent", new AbortController().signal),
    ).rejects.toThrow("ETag");
  });
  it("bind использует exact agent 17 / binding 4 и новую revision", async () => {
    client.put.mockResolvedValue({
      data: {
        ...binding,
        version: 5,
        revisionRef: "revision_new",
        digest: "b".repeat(64),
      },
    });
    await changeBinding(
      snapshot,
      "skills",
      "skill",
      { ref: "revision_new", digest: "b".repeat(64) },
      "bind",
      new AbortController().signal,
    );
    expect(client.put).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/agents/{agentRef}/skill-bundles/{bundleRef}",
        path: { agentRef: "agent", bundleRef: "skill" },
        body: { revisionRef: "revision_new", expectedBindingVersion: 4 },
        headers: {
          "Content-Type": "application/json",
          "If-Match": '"17"',
          "Idempotency-Key": "synthetic-key",
          "X-CSRF-Token": "synthetic-csrf",
        },
      }),
    );
  });
  it("unbind передает старую связанную revision, а не новую current resource revision", async () => {
    client.delete.mockResolvedValue({ data: { ...binding, version: 5 } });
    await changeBinding(
      snapshot,
      "skills",
      "skill",
      { ref: "revision_new", digest: "b".repeat(64) },
      "unbind",
      new AbortController().signal,
    );
    expect(client.delete).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { revisionRef: "revision_old", expectedBindingVersion: 4 },
      }),
    );
  });
  it("пустой авторитетный список допускает version 0, но не угадывает receipt version 1", async () => {
    const receipt = {
      ...binding,
      resourceRef: "memory",
      revisionRef: "memory_revision",
      version: 9,
    };
    client.put.mockResolvedValue({ data: receipt });
    expect(
      await changeBinding(
        snapshot,
        "memory",
        "memory",
        { ref: "memory_revision", digest: binding.digest },
        "bind",
        new AbortController().signal,
      ),
    ).toEqual(receipt);
    expect(client.put).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { revisionRef: "memory_revision", expectedBindingVersion: 0 },
      }),
    );
  });
  it("не повторяет mutation при чужой или неполной квитанции", async () => {
    client.put.mockResolvedValue({
      data: { ...binding, version: 5, agentRef: "foreign" },
    });
    await expect(
      changeBinding(
        snapshot,
        "skills",
        "skill",
        { ref: binding.revisionRef, digest: binding.digest },
        "bind",
        new AbortController().signal,
      ),
    ).rejects.toThrow("receipt");
    expect(client.put).toHaveBeenCalledTimes(1);
  });
});
