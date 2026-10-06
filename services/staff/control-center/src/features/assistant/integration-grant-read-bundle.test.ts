import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  createRenderer,
  defineComponent,
  h,
  ssrContextKey,
  type App,
  type Ref,
  type SetupContext,
} from "vue";
import type {
  IntegrationGrantCapabilityCandidatePage,
  ListIntegrationGrantCapabilityCandidatesData,
} from "@/shared/api/generated/openapi/types.gen";
const api = vi.hoisted(() => ({
  getIntegrationConnection: vi.fn(),
  getAgent: vi.fn(),
  getWorkflow: vi.fn(),
  listIntegrationGrantCapabilityCandidates: vi.fn(),
  owner: new AbortController(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/owner-lifetime", () => ({
  ownerRequestSignal: () => api.owner.signal,
  assertOwnerRequest: (signal: AbortSignal) => signal.throwIfAborted(),
}));
import { createIntegrationGrantReadBundle } from "./integration-grant-read-bundle";
import { editableOperations } from "./model";
import GrantForm from "./components/AssistantIntegrationGrantPlanForm.vue";

const address = {
  projectRef: "project_test",
  connectionRef: "connection_test",
  recipientKind: "AGENT" as const,
  recipientRef: "agent_test",
  connectionVersion: 4,
};
const pins = {
  contextDigest: "a".repeat(64),
  connectionVersion: 4,
  recipientVersion: 2,
  projectVersion: 1,
  definitionVersion: "1.0",
  definitionDigest: "b".repeat(64),
};
function page(start = 0, end = 42): IntegrationGrantCapabilityCandidatePage {
  return {
    context: {
      projectRef: address.projectRef,
      connectionRef: address.connectionRef,
      recipientKind: address.recipientKind,
      recipientRef: address.recipientRef,
    },
    contextDigest: pins.contextDigest,
    pins,
    total: 42,
    items: Array.from({ length: end - start }, (_, offset) => ({
      capability: {
        key: `github.read.${String(start + offset)}`,
        name: `Чтение ${String(start + offset)}`,
        description: "",
        risk: "READ",
        approvalRequired: false,
        approvalPolicy: "NONE",
        allowedApprovalPolicies: ["NONE"],
        operation: "READ",
        resourceKind: "GITHUB_REPOSITORY",
        inputFields: [],
      },
      grantable: true,
      reason: "READY",
      pins,
    })),
  };
}
const ok = (data: unknown) => ({
  data,
  response: new Response(null, { status: 200 }),
});
function defined<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Missing synthetic fixture");
  return value;
}
function candidateCall(index: number) {
  return api.listIntegrationGrantCapabilityCandidates.mock.calls[
    index
  ]?.[0] as unknown as
    | {
        query: ListIntegrationGrantCapabilityCandidatesData["query"];
        signal: AbortSignal;
      }
    | undefined;
}
const apps: App[] = [];
afterEach(() => {
  for (const app of apps.splice(0)) app.unmount();
});
function mountedBatch(count = 19) {
  const bundle = createIntegrationGrantReadBundle();
  const states: {
    valid: Ref<boolean>;
    expanded: Ref<boolean>;
    loading: Ref<boolean>;
    candidate: Ref<unknown>;
  }[] = [];
  const renderer = createRenderer<Record<string, never>, Record<string, never>>(
    {
      patchProp() {},
      insert() {},
      remove() {},
      createElement: () => ({}),
      createText: () => ({}),
      createComment: () => ({}),
      setText() {},
      setElementText() {},
      parentNode: () => null,
      nextSibling: () => null,
    },
  );
  const original = (
    GrantForm as unknown as {
      setup: (
        props: Record<string, unknown>,
        context: SetupContext,
      ) => (typeof states)[number];
    }
  ).setup;
  for (let index = 0; index < count; index++) {
    const parameters = {
      connectionRef: address.connectionRef,
      agentRef: address.recipientRef,
      capabilityKey: defined(page().items[index]).capability.key,
      enabled: true,
      approvalPolicy: "NONE",
      approvalScopePaths: [],
    };
    const operation = defined(
      editableOperations([
        {
          ref: `operation_${String(index)}`,
          type: "CHANGE_INTEGRATION_GRANT",
          action: "UPDATE",
          title: "Выдать разрешение",
          summary: "",
          target: {
            kind: "INTEGRATION_CONNECTION",
            ref: address.connectionRef,
            version: 4,
            name: "GitHub",
          },
          expectedVersion: 4,
          parameters,
          before: {},
          after: { ...parameters },
          selected: true,
          permitted: true,
          validationProblems: [],
        },
      ])[0],
    );
    const app = renderer.createApp(
      defineComponent({
        emits: ["valid", "dirty", "parameter"],
        setup(_props, context) {
          states.push(
            original(
              {
                operation,
                projectRef: address.projectRef,
                disabled: false,
                compact: true,
                readBundle: bundle,
              },
              context,
            ),
          );
          return () => h("harness");
        },
      }),
    );
    app.provide(ssrContextKey, { modules: new Set<string>() });
    app.mount({});
    apps.push(app);
  }
  return { bundle, states };
}
beforeEach(() => {
  vi.clearAllMocks();
  api.owner = new AbortController();
  api.getIntegrationConnection.mockResolvedValue(
    ok({
      ref: address.connectionRef,
      version: 4,
      name: "GitHub",
      capabilities: page().items.map((item) => item.capability),
      grants: [],
    }),
  );
  api.getAgent.mockResolvedValue(
    ok({
      ref: address.recipientRef,
      projectRef: address.projectRef,
      name: "Менеджер",
    }),
  );
  api.listIntegrationGrantCapabilityCandidates.mockResolvedValue(ok(page()));
});

describe("Пакетные чтения прав одной ревизии плана", () => {
  it("все19 свернутых mounted форм проверяются без открытия, одна общая загрузка42", async () => {
    const { bundle, states } = mountedBatch();
    await vi.waitFor(() =>
      expect(states.every((state) => !state.loading.value)).toBe(true),
    );
    expect(
      states.every((state) => state.valid.value && !state.expanded.value),
    ).toBe(true);
    expect(api.getIntegrationConnection).toHaveBeenCalledTimes(1);
    expect(api.getAgent).toHaveBeenCalledTimes(1);
    expect(api.listIntegrationGrantCapabilityCandidates).toHaveBeenCalledTimes(
      1,
    );
    bundle.close();
  });
  it("недоступная capability оставляет именно свою свернутую форму невалидной", async () => {
    const result = page();
    result.items[3] = {
      ...defined(result.items[3]),
      grantable: false,
      reason: "GRANT_UNAVAILABLE",
    };
    api.listIntegrationGrantCapabilityCandidates.mockResolvedValue(ok(result));
    const { bundle, states } = mountedBatch();
    await vi.waitFor(() =>
      expect(states.every((state) => !state.loading.value)).toBe(true),
    );
    expect(states.filter((state) => state.valid.value)).toHaveLength(18);
    expect(states[3]?.valid.value).toBe(false);
    expect(states.every((state) => !state.expanded.value)).toBe(true);
    bundle.close();
  });
  it("failed common read не позволяет применять ни одну свернутую форму", async () => {
    api.getIntegrationConnection.mockRejectedValue(new Error("Unavailable"));
    const { bundle, states } = mountedBatch();
    await vi.waitFor(() =>
      expect(states.every((state) => !state.loading.value)).toBe(true),
    );
    expect(states.every((state) => !state.valid.value)).toBe(true);
    expect(api.getIntegrationConnection).toHaveBeenCalledTimes(1);
    expect(api.listIntegrationGrantCapabilityCandidates).not.toHaveBeenCalled();
    bundle.close();
  });
  it("19 разных capabilities используют один connection, recipient и полный каталог42 без фильтра", async () => {
    const bundle = createIntegrationGrantReadBundle();
    const snapshots = await Promise.all(
      Array.from({ length: 19 }, () =>
        bundle.read(address, new AbortController().signal),
      ),
    );
    expect(api.getIntegrationConnection).toHaveBeenCalledTimes(1);
    expect(api.getAgent).toHaveBeenCalledTimes(1);
    expect(api.listIntegrationGrantCapabilityCandidates).toHaveBeenCalledTimes(
      1,
    );
    expect(candidateCall(0)?.query).toEqual({
      ...page().context,
      query: "",
      pageSize: 100,
      pageToken: undefined,
    });
    expect(
      snapshots.every((snapshot) => snapshot.candidates.length === 42),
    ).toBe(true);
    expect(new Set(snapshots.map((snapshot) => snapshot)).size).toBe(1);
    bundle.close();
  });
  it("полностью читает bounded pagination с неизменными pins", async () => {
    api.listIntegrationGrantCapabilityCandidates
      .mockResolvedValueOnce(
        ok({ ...page(0, 20), nextPageToken: "cursor_one" }),
      )
      .mockResolvedValueOnce(ok(page(20, 42)));
    const bundle = createIntegrationGrantReadBundle();
    expect(
      (await bundle.read(address, new AbortController().signal)).candidates,
    ).toHaveLength(42);
    expect(candidateCall(1)?.query.pageToken).toBe("cursor_one");
    bundle.close();
  });
  it("subscriber abort не отменяет соседнюю строку", async () => {
    let resolve!: (value: ReturnType<typeof ok>) => void;
    api.listIntegrationGrantCapabilityCandidates.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const bundle = createIntegrationGrantReadBundle();
    const subscriber = new AbortController();
    const abandoned = bundle.read(address, subscriber.signal);
    const abandonedResult = expect(abandoned).rejects.toThrow("aborted");
    const neighbor = bundle.read(address, new AbortController().signal);
    await vi.waitFor(() => expect(resolve).toBeTypeOf("function"));
    subscriber.abort();
    await abandonedResult;
    expect(candidateCall(0)?.signal.aborted).toBe(false);
    resolve(ok(page()));
    expect((await neighbor).candidates).toHaveLength(42);
    bundle.close();
  });
  it("закрытие ревизии и owner lifetime запрещают cached ACK и новый запрос", async () => {
    const bundle = createIntegrationGrantReadBundle();
    await bundle.read(address, new AbortController().signal);
    api.owner.abort();
    expect(() => bundle.read(address, new AbortController().signal)).toThrow();
    expect(api.getAgent).toHaveBeenCalledTimes(1);
    api.owner = new AbortController();
    const nextRevision = createIntegrationGrantReadBundle();
    await nextRevision.read(address, new AbortController().signal);
    expect(api.getAgent).toHaveBeenCalledTimes(2);
    nextRevision.close();
    expect(() =>
      nextRevision.read(address, new AbortController().signal),
    ).toThrow();
  });
  it("owner flip закрывает незавершённый read; поздний ACK не принимается соседними строками", async () => {
    let resolve!: (value: ReturnType<typeof ok>) => void;
    api.getAgent.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const bundle = createIntegrationGrantReadBundle();
    const read = bundle.read(address, new AbortController().signal);
    const rejected = expect(read).rejects.toThrow("aborted");
    api.owner.abort();
    await rejected;
    resolve(ok({ ref: address.recipientRef, projectRef: address.projectRef }));
    await Promise.resolve();
    expect(api.listIntegrationGrantCapabilityCandidates).not.toHaveBeenCalled();
    expect(() => bundle.read(address, new AbortController().signal)).toThrow();
    bundle.close();
  });
  it("отделяет recipient и project keys и не принимает чужой project readback", async () => {
    const bundle = createIntegrationGrantReadBundle();
    await bundle.read(address, new AbortController().signal);
    await expect(
      bundle.read(
        { ...address, projectRef: "foreign" },
        new AbortController().signal,
      ),
    ).rejects.toThrow("mismatch");
    await expect(
      bundle.read(
        { ...address, recipientRef: "foreign" },
        new AbortController().signal,
      ),
    ).rejects.toThrow("mismatch");
    expect(api.getAgent).toHaveBeenCalledTimes(3);
    bundle.close();
  });
  it("текущие имена доступны при version drift, но ни одного candidate не выдаётся", async () => {
    api.getIntegrationConnection.mockResolvedValue(
      ok({ ref: address.connectionRef, version: 5, name: "GitHub" }),
    );
    const bundle = createIntegrationGrantReadBundle();
    const snapshot = await bundle.read(address, new AbortController().signal);
    expect(snapshot.connection.version).toBe(5);
    expect(snapshot.candidates).toEqual([]);
    expect(api.listIntegrationGrantCapabilityCandidates).not.toHaveBeenCalled();
    bundle.close();
  });
  it.each([
    { context: { ...page().context, projectRef: "foreign" } },
    { total: 43 },
    { total: 1001 },
    { pins: { ...pins, connectionVersion: 5 } },
    { items: [page().items[0], page().items[0]] },
    { nextPageToken: "repeated" },
  ])(
    "fail closed при чужом/неполном/повторяющемся каталоге %j",
    async (changed) => {
      api.listIntegrationGrantCapabilityCandidates.mockResolvedValue(
        ok({ ...page(), ...changed }),
      );
      const bundle = createIntegrationGrantReadBundle();
      await expect(
        bundle.read(address, new AbortController().signal),
      ).rejects.toThrow();
      bundle.close();
    },
  );
  it("grantable=false не становится разрешённым после общего чтения", async () => {
    const unavailable = page();
    unavailable.items[0] = {
      ...defined(unavailable.items[0]),
      grantable: false,
      reason: "GRANT_UNAVAILABLE",
    };
    api.listIntegrationGrantCapabilityCandidates.mockResolvedValue(
      ok(unavailable),
    );
    const bundle = createIntegrationGrantReadBundle();
    expect(
      (await bundle.read(address, new AbortController().signal)).candidates[0]
        ?.grantable,
    ).toBe(false);
    bundle.close();
  });
});
