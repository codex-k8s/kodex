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

const api = vi.hoisted(() => ({
  getIntegrationConnection: vi.fn(),
  getSystemAssistant: vi.fn(),
  getAgent: vi.fn(),
  getSystemAssistantIntegrationGrantCandidates: vi.fn(),
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
import Form from "./AssistantSystemIntegrationGrantPlanForm.vue";

const pins = {
  connectionRef: "connection_test",
  capabilityKey: "context7.docs.query",
  scopeKind: "ORGANIZATION",
  organizationRef: "organization_test",
  systemAssistantRef: "assistant_test",
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
  type: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
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
  assistantRef: pins.systemAssistantRef,
  assistantVersion: 9,
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
}
const apps: App[] = [];
afterEach(() => {
  for (const app of apps.splice(0)) app.unmount();
});
async function setup(): Promise<State> {
  const editable = editableOperations([operation])[0];
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
      emits: ["valid", "dirty", "parameter"],
      setup(_props, context) {
        state = original({ operation: editable, disabled: false }, context);
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
describe("Форма exact SYSTEM grant plan", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.owner = new AbortController();
    api.getIntegrationConnection.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.connectionRef,
        version: 10,
        definitionVersion: pins.definitionVersion,
        definitionDigest: pins.definitionDigest,
      },
    });
    api.getSystemAssistant.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: { ref: pins.systemAssistantRef, version: 1014 },
    });
    api.getAgent.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.systemAssistantRef,
        version: 9,
        system: true,
        enabled: true,
        projectRef: "",
      },
    });
    api.getSystemAssistantIntegrationGrantCandidates.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: structuredClone(page),
    });
  });
  it("разрешает exact READ/NONE после Agent9 readback при SystemAssistant1014 и отсутствующем cursor", async () => {
    const state = await setup();
    expect(state.ownerValid.value).toBe(true);
    expect(state.failed.value).toBe(false);
    expect(state.valid.value).toBe(true);
    expect(state.candidate.value).toEqual(candidate);
    expect(api.getAgent).toHaveBeenCalledWith({
      path: { agentRef: pins.systemAssistantRef },
      signal: expect.any(AbortSignal) as unknown,
      cache: "no-store",
    });
  });
  it.each([
    { assistantVersion: 10 },
    { organizationRef: "foreign" },
    { connectionVersion: 11 },
    { definitionDigest: "b".repeat(64) },
    { nextPageToken: null },
  ])("сохраняет закрытый отказ при несовместимых pins %j", async (changed) => {
    api.getSystemAssistantIntegrationGrantCandidates.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: { ...page, ...changed },
    });
    const state = await setup();
    expect(state.failed.value).toBe(true);
    expect(state.valid.value).toBe(false);
    expect(state.candidate.value).toBeUndefined();
  });
  it("не принимает PROJECT Agent вместо собственной SYSTEM identity", async () => {
    api.getAgent.mockResolvedValue({
      response: new Response(null, { status: 200 }),
      data: {
        ref: pins.systemAssistantRef,
        version: 9,
        system: false,
        enabled: true,
        projectRef: "project_foreign",
      },
    });
    const state = await setup();
    expect(state.failed.value).toBe(true);
    expect(state.valid.value).toBe(false);
    expect(
      api.getSystemAssistantIntegrationGrantCandidates,
    ).not.toHaveBeenCalled();
  });
});

