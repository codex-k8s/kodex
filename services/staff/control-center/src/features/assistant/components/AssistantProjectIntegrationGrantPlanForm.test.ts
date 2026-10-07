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
import { createI18n } from "vue-i18n";
import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";
import { editableOperations } from "../model";
import {
  commonProjectGrantBatchContext,
  projectGrantBatchContext,
  type ProjectGrantBatchContext,
} from "../project-grant-batch-context";

const api = vi.hoisted(() => ({
  getIntegrationConnection: vi.fn(),
  getProjectAssistant: vi.fn(),
  getAgent: vi.fn(),
  getProjectAssistantIntegrationGrantCandidates: vi.fn(),
  owner: new AbortController(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => api);
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({
    bootstrap: { organizationRef: "organization_test" },
  }),
}));
vi.mock("@/shared/api/owner-lifetime", () => ({
  ownerRequestSignal: () => api.owner.signal,
  assertOwnerRequest: (signal: AbortSignal) => signal.throwIfAborted(),
}));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({ mutate: vi.fn() }));
import Form from "./AssistantProjectIntegrationGrantPlanForm.vue";
import {
  createProjectIntegrationGrantReadBundle,
  type ProjectIntegrationGrantReadBundle,
} from "../project-integration-grant-read-bundle";

const pins = {
  connectionRef: "connection_test",
  capabilityKey: "context7.docs.query",
  scopeKind: "ORGANIZATION",
  assistantScope: "PROJECT",
  projectRef: "project_test",
  assistantProfileRef: "profile_test",
  agentVersion: 9,
  profileVersion: 1,
  organizationRef: "organization_test",
  projectAssistantRef: "assistant_test",
  definitionVersion: "1.0",
  definitionDigest: "a".repeat(64),
  grantRef: "",
  grantVersion: 0,
  defaultApprovalPolicy: "NONE",
  allowedApprovalPolicies: ["NONE"],
};
const before = {
  ...pins,
  enabled: false,
  approvalPolicy: "NONE",
  approvalScopePaths: [],
};
const after = { ...before, enabled: true };
const operation: AssistantPlanOperationInput = {
  ref: "operation_test",
  type: "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
  action: "UPDATE",
  title: "Context7",
  summary: "Доступ к документации",
  target: {
    kind: "INTEGRATION_CONNECTION",
    ref: pins.connectionRef,
    version: 10,
    name: "Context7",
  },
  expectedVersion: 10,
  selected: true,
  permitted: true,
  validationProblems: [],
  parameters: after,
  before,
  after,
};
const candidate = {
  capability: {
    key: pins.capabilityKey,
    name: "Документация",
    description: "",
    risk: "READ",
    approvalRequired: false,
    operation: "READ",
    resourceKind: "MANAGED_MCP",
    inputFields: [],
    approvalPolicy: "NONE",
    allowedApprovalPolicies: ["NONE"],
  },
  grantable: true,
  reason: "READY",
  currentGrantVersion: 0,
  currentGrantEnabled: false,
  currentApprovalScopePaths: [],
};
const page = {
  scopeKind: "ORGANIZATION",
  organizationRef: pins.organizationRef,
  projectRef: pins.projectRef,
  assistantProfileRef: pins.assistantProfileRef,
  assistantRef: pins.projectAssistantRef,
  assistantVersion: 9,
  profileVersion: 1,
  connectionRef: pins.connectionRef,
  connectionVersion: 10,
  definitionVersion: pins.definitionVersion,
  definitionDigest: pins.definitionDigest,
  items: [candidate],
  total: 1,
};
interface State {
  loading: Ref<boolean>;
  failed: Ref<boolean>;
  valid: Ref<boolean>;
  ownerValid: Ref<boolean>;
  candidate: Ref<unknown>;
  expanded: Ref<boolean>;
  batchContext: Ref<ProjectGrantBatchContext | undefined>;
}
const apps: App[] = [];
afterEach(() => {
  for (const app of apps.splice(0)) app.unmount();
});
async function setup(
  options: {
    operation?: AssistantPlanOperationInput;
    readBundle?: ProjectIntegrationGrantReadBundle;
    compact?: boolean;
    appliedGrantRef?: string;
  } = {},
): Promise<State> {
  const editable = editableOperations([options.operation ?? operation])[0];
  if (!editable) throw new Error("Missing operation fixture");
  let state!: State;
  // Настоящий mounted watcher: SSR закрывает onCleanup до async readback.
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
    Form as unknown as {
      setup: (props: Record<string, unknown>, context: SetupContext) => State;
    }
  ).setup;
  const app = renderer.createApp(
    defineComponent({
      emits: ["valid", "dirty", "parameter", "context", "expanded"],
      setup(_props, context) {
        state = original(
          { ...options, operation: editable, disabled: false },
          context,
        );
        return () => h("harness");
      },
    }),
  );
  app.use(createI18n({ legacy: false, locale: "ru" }));
  app.provide(ssrContextKey, { modules: new Set<string>() });
  app.mount({});
  apps.push(app);
  await vi.waitFor(() => expect(state.loading.value).toBe(false));
  return state;
}
describe("Форма exact PROJECT grant plan", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.owner = new AbortController();
    api.getIntegrationConnection.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.connectionRef,
        name: "Документация проекта",
        version: 10,
        definitionVersion: pins.definitionVersion,
        definitionDigest: pins.definitionDigest,
      },
    });
    api.getProjectAssistant.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.assistantProfileRef,
        version: 1,
        agentRef: pins.projectAssistantRef,
        projectRef: pins.projectRef,
        state: "ACTIVE",
      },
    });
    api.getAgent.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.projectAssistantRef,
        version: 9,
        system: false,
        enabled: true,
        projectRef: pins.projectRef,
      },
    });
    api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: structuredClone(page),
    });
  });
  it("разрешает exact READ/NONE после exact Profile1/Agent9 readback и отсутствующем cursor", async () => {
    const state = await setup();
    expect(state.ownerValid.value).toBe(true);
    expect(state.failed.value).toBe(false);
    expect(state.valid.value).toBe(true);
    expect(state.candidate.value).toEqual(candidate);
    expect(api.getAgent).toHaveBeenCalledWith({
      path: { agentRef: pins.projectAssistantRef },
      signal: expect.any(AbortSignal) as unknown,
      cache: "no-store",
    });
  });
  it.each([
    { assistantVersion: 10 },
    { organizationRef: "foreign" },
    { projectRef: "foreign_project" },
    { assistantProfileRef: "foreign_profile" },
    { profileVersion: 2 },
    { connectionVersion: 11 },
    { definitionDigest: "b".repeat(64) },
    { nextPageToken: null },
  ])("сохраняет закрытый отказ при несовместимых pins %j", async (changed) => {
    api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: { ...page, ...changed },
    });
    const state = await setup();
    expect(state.failed.value).toBe(true);
    expect(state.valid.value).toBe(false);
    expect(state.candidate.value).toBeUndefined();
  });
  it("не принимает SYSTEM Agent вместо собственной PROJECT identity", async () => {
    api.getAgent.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.projectAssistantRef,
        version: 9,
        system: true,
        enabled: true,
        projectRef: "",
      },
    });
    const state = await setup();
    expect(state.failed.value).toBe(true);
    expect(state.valid.value).toBe(false);
    expect(
      api.getProjectAssistantIntegrationGrantCandidates,
    ).not.toHaveBeenCalled();
  });
  function batchOperation(index: number): AssistantPlanOperationInput {
    const next = structuredClone(operation);
    next.ref = `operation_${String(index)}`;
    for (const snapshot of [next.parameters, next.before, next.after])
      snapshot.capabilityKey = `github.read.${String(index)}`;
    return next;
  }
  function requiredOperation(
    operations: AssistantPlanOperationInput[],
    index: number,
  ): AssistantPlanOperationInput {
    const operation = operations[index];
    if (!operation) throw new Error("Missing operation fixture");
    return operation;
  }
  function batchPage(start = 0, end = 24) {
    return {
      ...page,
      total: 24,
      items: Array.from({ length: end - start }, (_, offset) => ({
        ...structuredClone(candidate),
        capability: {
          ...candidate.capability,
          key: `github.read.${String(start + offset)}`,
          name: `Чтение ${String(start + offset)}`,
        },
      })),
    };
  }
  const ok = (data: unknown) => ({
    data,
    response: new Response(null, { status: 200 }),
  });
  async function batch() {
    const readBundle = createProjectIntegrationGrantReadBundle();
    const states = await Promise.all(
      Array.from({ length: 20 }, (_, index) =>
        setup({ operation: batchOperation(index), readBundle, compact: true }),
      ),
    );
    return { readBundle, states };
  }
  it("все20 свернутых PROJECT форм проверяются по одному connection/Profile/Agent/полному каталогу24", async () => {
    api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue(
      ok(batchPage()),
    );
    const { readBundle, states } = await batch();
    expect(
      states.every((state) => state.valid.value && !state.expanded.value),
    ).toBe(true);
    const operations = Array.from({ length: 20 }, (_, index) =>
      batchOperation(index),
    );
    const contexts = Object.fromEntries(
      states.map((state, index) => [
        requiredOperation(operations, index).ref,
        state.batchContext.value,
      ]),
    );
    expect(
      commonProjectGrantBatchContext(operations, contexts, pins.organizationRef)
        ?.connectionName,
    ).toBe("Документация проекта");
    expect(
      commonProjectGrantBatchContext(
        operations,
        { ...contexts, operation_7: undefined },
        pins.organizationRef,
      ),
    ).toBeUndefined();
    expect(
      commonProjectGrantBatchContext(operations, contexts, undefined),
    ).toBeUndefined();
    expect(
      commonProjectGrantBatchContext(operations, contexts, "foreign_org"),
    ).toBeUndefined();
    const changed = structuredClone(operations);
    requiredOperation(changed, 7).parameters.enabled = false;
    requiredOperation(changed, 7).after.enabled = false;
    expect(
      commonProjectGrantBatchContext(changed, contexts, pins.organizationRef),
    ).toBeUndefined();
    api.owner.abort();
    expect(
      states.every((state) => state.batchContext.value === undefined),
    ).toBe(true);
    expect(api.getIntegrationConnection).toHaveBeenCalledTimes(1);
    expect(api.getProjectAssistant).toHaveBeenCalledTimes(1);
    expect(api.getAgent).toHaveBeenCalledTimes(1);
    expect(
      api.getProjectAssistantIntegrationGrantCandidates,
    ).toHaveBeenCalledTimes(1);
    expect(
      api.getProjectAssistantIntegrationGrantCandidates,
    ).toHaveBeenCalledWith({
      path: { projectRef: pins.projectRef },
      query: {
        connectionRef: pins.connectionRef,
        query: "",
        pageToken: undefined,
        pageSize: 100,
      },
      signal: expect.any(AbortSignal) as unknown,
      cache: "no-store",
    });
    readBundle.close();
  });
  it("bounded full pagination выполняется однажды для20строк", async () => {
    api.getProjectAssistantIntegrationGrantCandidates
      .mockResolvedValueOnce(
        ok({ ...batchPage(0, 10), nextPageToken: "cursor_one" }),
      )
      .mockResolvedValueOnce(ok(batchPage(10, 24)));
    const { readBundle, states } = await batch();
    expect(states.every((state) => state.valid.value)).toBe(true);
    expect(
      api.getProjectAssistantIntegrationGrantCandidates,
    ).toHaveBeenCalledTimes(2);
    readBundle.close();
  });
  it("один неправильный immutable grant pin закрывает только эту строку после общего каталога", async () => {
    const catalog = batchPage();
    const row = catalog.items[3];
    if (!row) throw new Error("Missing fixture");
    row.currentGrantVersion = 1;
    const result = {
      ...row,
      currentGrantRef: "foreign_grant",
      currentApprovalPolicy: "NONE",
    };
    catalog.items[3] = result;
    api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue(
      ok(catalog),
    );
    const { readBundle, states } = await batch();
    expect(states.filter((state) => state.valid.value)).toHaveLength(19);
    expect(states[3]?.valid.value).toBe(false);
    expect(states[3]?.failed.value).toBe(true);
    expect(states[3]?.batchContext.value).toBeUndefined();
    readBundle.close();
  });
  it.each([
    { projectRef: "other_project" },
    { assistantProfileRef: "other_profile" },
    { projectAssistantRef: "other_helper" },
    { agentVersion: 10 },
    { profileVersion: 2 },
    { connectionRef: "other_connection" },
    { definitionVersion: "2.0" },
    { definitionDigest: "b".repeat(64) },
    { enabled: false },
    { approvalScopePaths: ["owner"] },
  ])(
    "общая подпись не объединяет различные owner/policy pins %j",
    (changed) => {
      const operations = [batchOperation(0), batchOperation(1)];
      for (const snapshot of [
        requiredOperation(operations, 1).parameters,
        requiredOperation(operations, 1).before,
        requiredOperation(operations, 1).after,
      ])
        Object.assign(snapshot, changed);
      if (changed.connectionRef)
        requiredOperation(operations, 1).target.ref = changed.connectionRef;
      const contexts = Object.fromEntries(
        operations.map((operation) => [
          operation.ref,
          projectGrantBatchContext(
            operation,
            pins.organizationRef,
            "Документация проекта",
          ),
        ]),
      );
      expect(
        commonProjectGrantBatchContext(
          operations,
          contexts,
          pins.organizationRef,
        ),
      ).toBeUndefined();
    },
  );
  it("общая подпись закрыта для одной/смешанной операции и различного проверенного имени", () => {
    const operations = [batchOperation(0), batchOperation(1)];
    const contexts = Object.fromEntries(
      operations.map((operation) => [
        operation.ref,
        projectGrantBatchContext(
          operation,
          pins.organizationRef,
          "Документация проекта",
        ),
      ]),
    );
    expect(
      commonProjectGrantBatchContext(
        [requiredOperation(operations, 0)],
        contexts,
        pins.organizationRef,
      ),
    ).toBeUndefined();
    contexts.operation_1 = {
      ...contexts.operation_1,
      key: contexts.operation_1?.key ?? "",
      connectionName: "Другое подключение",
    };
    expect(
      commonProjectGrantBatchContext(
        operations,
        contexts,
        pins.organizationRef,
      ),
    ).toBeUndefined();
    requiredOperation(operations, 1).type = "CHANGE_INTEGRATION_GRANT";
    expect(
      commonProjectGrantBatchContext(
        operations,
        contexts,
        pins.organizationRef,
      ),
    ).toBeUndefined();
  });
  it.each([
    { total: 25 },
    { total: 1001 },
    { assistantVersion: 10 },
    { assistantProfileRef: "foreign" },
    { definitionDigest: "b".repeat(64) },
    { projectRef: "foreign" },
  ])(
    "20строк failclosed вместе при изменившемся каталоге %j",
    async (changed) => {
      api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue(
        ok({ ...batchPage(), ...changed }),
      );
      const { readBundle, states } = await batch();
      expect(
        states.every((state) => !state.valid.value && state.failed.value),
      ).toBe(true);
      expect(
        api.getProjectAssistantIntegrationGrantCandidates,
      ).toHaveBeenCalledTimes(1);
      readBundle.close();
    },
  );
  it("subscriber abort не отменяет чтение другой PROJECT строки", async () => {
    let resolve!: (response: ReturnType<typeof ok>) => void;
    api.getProjectAssistantIntegrationGrantCandidates.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const readBundle = createProjectIntegrationGrantReadBundle();
    const subscriber = new AbortController();
    const abandoned = readBundle.read(
      batchOperation(0),
      pins.organizationRef,
      subscriber.signal,
    );
    const rejected = expect(abandoned).rejects.toThrow("aborted");
    const neighbor = readBundle.read(
      batchOperation(1),
      pins.organizationRef,
      new AbortController().signal,
    );
    await vi.waitFor(() => expect(resolve).toBeTypeOf("function"));
    subscriber.abort();
    await rejected;
    resolve(ok(batchPage()));
    expect((await neighbor).page.items).toHaveLength(24);
    readBundle.close();
  });
  it("owner flip закрывает late ACK и новый cached read, новая ревизия выполняет свежий owner read", async () => {
    const readBundle = createProjectIntegrationGrantReadBundle();
    let resolve!: (response: ReturnType<typeof ok>) => void;
    api.getProjectAssistantIntegrationGrantCandidates.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const pending = readBundle.read(
      batchOperation(0),
      pins.organizationRef,
      new AbortController().signal,
    );
    const rejected = expect(pending).rejects.toThrow("aborted");
    await vi.waitFor(() => expect(resolve).toBeTypeOf("function"));
    api.owner.abort();
    await rejected;
    resolve(ok(batchPage()));
    expect(() =>
      readBundle.read(
        batchOperation(1),
        pins.organizationRef,
        new AbortController().signal,
      ),
    ).toThrow();
    api.owner = new AbortController();
    api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue(
      ok(batchPage()),
    );
    const next = createProjectIntegrationGrantReadBundle();
    expect(
      (
        await next.read(
          batchOperation(1),
          pins.organizationRef,
          new AbortController().signal,
        )
      ).page.total,
    ).toBe(24);
    expect(api.getProjectAssistant).toHaveBeenCalledTimes(2);
    next.close();
    readBundle.close();
  });
  it("не принимает чужой bootstrap/mixedSYSTEM/readonlytamper и не делает network fallback", async () => {
    const readBundle = createProjectIntegrationGrantReadBundle();
    const changed = batchOperation(0);
    changed.parameters.assistantScope = "SYSTEM";
    await expect(
      readBundle.read(
        changed,
        pins.organizationRef,
        new AbortController().signal,
      ),
    ).rejects.toThrow("owner");
    await expect(
      readBundle.read(
        batchOperation(0),
        "foreign_org",
        new AbortController().signal,
      ),
    ).rejects.toThrow("owner");
    await expect(
      readBundle.read(
        batchOperation(0),
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow("owner");
    expect(api.getIntegrationConnection).not.toHaveBeenCalled();
    expect(api.getProjectAssistant).not.toHaveBeenCalled();
    readBundle.close();
  });
  it.each([
    { projectRef: "foreign_project" },
    { assistantProfileRef: "foreign_profile" },
    { projectAssistantRef: "foreign_helper" },
    { agentVersion: 10 },
    { profileVersion: 2 },
    { definitionVersion: "2.0" },
    { definitionDigest: "b".repeat(64) },
  ])(
    "не использует cached catalog для другого owner/dependency tuple %j",
    async (changed) => {
      api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue(
        ok(batchPage()),
      );
      const readBundle = createProjectIntegrationGrantReadBundle();
      await readBundle.read(
        batchOperation(0),
        pins.organizationRef,
        new AbortController().signal,
      );
      const other = batchOperation(1);
      for (const snapshot of [other.parameters, other.before, other.after])
        Object.assign(snapshot, changed);
      await expect(
        readBundle.read(
          other,
          pins.organizationRef,
          new AbortController().signal,
        ),
      ).rejects.toThrow();
      expect(api.getIntegrationConnection).toHaveBeenCalledTimes(2);
      readBundle.close();
    },
  );
  it("APPLIED readback допускает только exact receipt и выбранную current grant строку", async () => {
    const current = batchPage();
    const row = current.items[0];
    if (!row) throw new Error("Missing fixture");
    const after = {
      ...row,
      currentGrantRef: "grant_applied",
      currentGrantVersion: 1,
      currentGrantEnabled: true,
      currentApprovalPolicy: "NONE",
    };
    current.items[0] = after;
    api.getIntegrationConnection.mockResolvedValue(
      ok({
        ref: pins.connectionRef,
        version: 30,
        definitionVersion: pins.definitionVersion,
        definitionDigest: pins.definitionDigest,
      }),
    );
    api.getProjectAssistantIntegrationGrantCandidates.mockResolvedValue(
      ok({ ...current, connectionVersion: 30 }),
    );
    const readBundle = createProjectIntegrationGrantReadBundle();
    const accepted = await setup({
      operation: batchOperation(0),
      appliedGrantRef: "grant_applied",
      readBundle,
      compact: true,
    });
    const rejected = await setup({
      operation: batchOperation(0),
      appliedGrantRef: "foreign_grant",
      readBundle,
      compact: true,
    });
    expect(accepted.valid.value).toBe(true);
    expect(rejected.valid.value).toBe(false);
    expect(
      api.getProjectAssistantIntegrationGrantCandidates,
    ).toHaveBeenCalledTimes(1);
    readBundle.close();
  });
});
