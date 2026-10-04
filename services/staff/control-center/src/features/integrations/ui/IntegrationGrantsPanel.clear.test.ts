import { defineComponent, type Ref, type SetupContext } from "vue";
import { createI18n } from "vue-i18n";
import { beforeEach, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type {
  IntegrationConnection,
  IntegrationGrantProjectCandidate,
  IntegrationGrantRecipientCandidate,
  IntegrationGrantCapabilityCandidate,
  IntegrationCapability,
} from "@/shared/api/generated/openapi/types.gen";

const loaders = vi.hoisted(() => ({
  connections: vi.fn(),
  projects: vi.fn(),
  recipients: vi.fn(),
  capabilities: vi.fn(),
}));
vi.mock("@/features/integrations/grant-candidates", () => ({
  connectionCandidates: () => loaders.connections,
  projectCandidates: () => loaders.projects,
  recipientCandidates: () => loaders.recipients,
  capabilityCandidates: () => loaders.capabilities,
}));
import IntegrationGrantsPanel from "./IntegrationGrantsPanel.vue";

const connection: IntegrationConnection = {
  ref: "connection",
  version: 3,
  definitionKey: "github",
  name: "GitHub",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: ["MANAGE_GRANTS"],
  definitionVersion: "2.3",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
const pins = { connectionVersion: 3, contextDigest: "b".repeat(64) };
const project: IntegrationGrantProjectCandidate = {
  projectRef: "project",
  name: "Проект",
  grantable: true,
  reason: "READY",
  pins,
};
const recipient: IntegrationGrantRecipientCandidate = {
  recipientRef: "agent",
  recipientKind: "AGENT",
  projectRef: "project",
  name: "Агент",
  grantable: true,
  reason: "READY",
  pins,
};
const capability: IntegrationGrantCapabilityCandidate = {
  capability: {
    key: "read",
    name: "Чтение",
    description: "",
    risk: "READ",
    approvalRequired: false,
    operation: "READ",
    resourceKind: "GITHUB_REPOSITORY",
    inputFields: [],
    approvalPolicy: "NONE",
    allowedApprovalPolicies: ["NONE"],
  },
  grantable: true,
  reason: "READY",
  pins,
};
interface State {
  clearProject(): void;
  clearRecipient(): void;
  clearCapability(): void;
  changeConnection(value: null): void;
  submit(): void;
  loadProjects(
    query: string,
    cursor: undefined,
    signal: AbortSignal,
  ): Promise<unknown>;
  chooseProject(option: { ref: string; title: string }): void;
  chooseRecipient(option: { ref: string; title: string }): void;
  loadRecipients(
    query: string,
    cursor: undefined,
    signal: AbortSignal,
  ): Promise<unknown>;
  loadCapabilities(
    query: string,
    cursor: undefined,
    signal: AbortSignal,
  ): Promise<unknown>;
  chooseCapability(option: { ref: string; title: string }): void;
  recipientContextKey: Ref<string>;
  capabilityContextKey: Ref<string>;
  approvalScopePaths: Ref<string[]>;
  selectedApprovalPolicy: Ref<
    IntegrationCapability["approvalPolicy"] | undefined
  >;
  availableApprovalPolicies: Ref<
    readonly IntegrationCapability["approvalPolicy"][]
  >;
  changeApprovalPolicy(value: string): void;
  projectCandidate: Ref<IntegrationGrantProjectCandidate | undefined>;
  recipientCandidate: Ref<IntegrationGrantRecipientCandidate | undefined>;
  capabilityCandidate: Ref<IntegrationGrantCapabilityCandidate | undefined>;
}
async function panel(selectedConnection = connection) {
  const emit = vi.fn();
  const setup = (
    IntegrationGrantsPanel as unknown as {
      setup: (
        props: Record<string, unknown>,
        context: SetupContext,
      ) => Record<string, unknown>;
    }
  ).setup;
  const state = await captureSetupState(
    defineComponent({
      setup(_props, context) {
        return setup(
          {
            grants: [],
            selectedConnection,
            projectRef: "project",
            targetKind: "AGENT",
            targetRef: "agent",
            capabilityKey: "read",
            busy: false,
          },
          { ...context, emit },
        );
      },
    }),
    (app) =>
      app.use(
        createI18n({ legacy: false, locale: "ru", messages: { ru: {} } }),
      ),
  );
  const result = state as unknown as State;
  result.projectCandidate.value = project;
  result.recipientCandidate.value = recipient;
  result.capabilityCandidate.value = capability;
  result.selectedApprovalPolicy.value = capability.capability.approvalPolicy;
  return { state: result, emit };
}
beforeEach(() => {
  vi.resetAllMocks();
  loaders.connections.mockResolvedValue({ items: [] });
});
it.each([
  "clearProject",
  "clearRecipient",
  "clearCapability",
  "changeConnection",
] as const)(
  "%s немедленно закрывает submit до обновления controlled props родителем",
  async (action) => {
    const { state, emit } = await panel();
    state.submit();
    expect(emit).toHaveBeenCalledWith(
      "save",
      expect.objectContaining({ capabilityKey: "read" }),
    );
    emit.mockClear();
    state[action](null);
    state.submit();
    expect(emit.mock.calls.some(([name]) => name === "save")).toBe(false);
    expect(state.capabilityCandidate.value).toBeUndefined();
    expect(state.selectedApprovalPolicy.value).toBeUndefined();
    expect(emit).toHaveBeenCalledWith("update:capabilityKey", "");
    if (action !== "clearCapability") {
      expect(state.recipientCandidate.value).toBeUndefined();
      expect(emit).toHaveBeenCalledWith("update:targetRef", "");
    }
    if (action === "clearProject" || action === "changeConnection") {
      expect(state.projectCandidate.value).toBeUndefined();
      expect(emit).toHaveBeenCalledWith("update:projectRef", "");
    }
    if (action === "changeConnection")
      expect(emit).toHaveBeenCalledWith("selectConnection", "");
  },
);
it("поздняя страница после очистки не восстанавливает candidate pins", async () => {
  let complete!: (value: unknown) => void;
  loaders.projects.mockImplementation(
    () =>
      new Promise((resolve) => {
        complete = resolve;
      }),
  );
  const { state, emit } = await panel();
  const loading = state.loadProjects(
    "",
    undefined,
    new AbortController().signal,
  );
  state.clearProject();
  complete({ items: [project], pins, total: 1 });
  await expect(loading).resolves.toEqual({ items: [] });
  emit.mockClear();
  state.chooseProject({ ref: "project", title: "Проект" });
  expect(state.projectCandidate.value).toBeUndefined();
  expect(emit).not.toHaveBeenCalled();
});

it("повторный выбор проекта с новой ревизией синхронно отзывает зависимый выбор", async () => {
  const { state, emit } = await panel();
  const previousContext = state.recipientContextKey.value;
  const refreshed = { ...project, pins: { ...pins, projectVersion: 2 } };
  loaders.projects.mockResolvedValue({ items: [refreshed], pins, total: 1 });
  await state.loadProjects("", undefined, new AbortController().signal);
  emit.mockClear();
  state.chooseProject({ ref: project.projectRef, title: project.name });
  state.submit();
  expect(state.projectCandidate.value).toEqual(refreshed);
  expect(state.recipientCandidate.value).toBeUndefined();
  expect(state.capabilityCandidate.value).toBeUndefined();
  expect(state.recipientContextKey.value).not.toBe(previousContext);
  expect(emit.mock.calls.some(([name]) => name === "save")).toBe(false);
  expect(emit).toHaveBeenCalledWith("update:targetRef", "");
  expect(emit).toHaveBeenCalledWith("update:capabilityKey", "");
});

it("повторный выбор получателя сбрасывает capability и её загруженные страницы", async () => {
  const { state, emit } = await panel();
  const previousContext = state.capabilityContextKey.value;
  const refreshed = { ...recipient, pins: { ...pins, recipientVersion: 2 } };
  loaders.recipients.mockResolvedValue({ items: [refreshed], pins, total: 1 });
  await state.loadRecipients("", undefined, new AbortController().signal);
  emit.mockClear();
  state.chooseRecipient({ ref: recipient.recipientRef, title: recipient.name });
  state.submit();
  expect(state.recipientCandidate.value).toEqual(refreshed);
  expect(state.capabilityCandidate.value).toBeUndefined();
  expect(state.capabilityContextKey.value).not.toBe(previousContext);
  expect(emit.mock.calls.some(([name]) => name === "save")).toBe(false);
  expect(emit).toHaveBeenCalledWith("update:capabilityKey", "");
});

it("подставляет действующую область Human Gate при повторном выборе разрешения", async () => {
  const selectedConnection: IntegrationConnection = {
    ...connection,
    grants: [
      {
        ref: "existing-grant",
        version: 2,
        capabilityKey: "read",
        agentRef: "agent",
        targetName: "Агент",
        enabled: true,
        risk: "WRITE",
        approvalPolicy: "HUMAN_SCOPED",
        approvalScopePaths: ["/body/id"],
        resourceScope: {
          kind: "HTTPS_RESOURCE",
          values: { host: "example.test" },
          digest: "c".repeat(64),
        },
      },
    ],
  };
  const scopedCapability: IntegrationGrantCapabilityCandidate = {
    ...capability,
    capability: {
      ...capability.capability,
      approvalPolicy: "HUMAN_SCOPED",
      allowedApprovalPolicies: ["HUMAN_SCOPED"],
      risk: "WRITE",
      approvalRequired: true,
      inputSchema: JSON.stringify({
        type: "object",
        properties: {
          body: { type: "object", properties: { id: { type: "integer" } } },
        },
      }),
    },
    currentGrantRef: "existing-grant",
    currentGrantVersion: 2,
  };
  loaders.capabilities.mockResolvedValue({
    items: [scopedCapability],
    total: 1,
  });
  const { state } = await panel(selectedConnection);
  await state.loadCapabilities("", undefined, new AbortController().signal);
  state.chooseCapability({ ref: "read", title: "Чтение" });
  expect(state.approvalScopePaths.value).toEqual(["/body/id"]);
  expect(state.selectedApprovalPolicy.value).toBe("HUMAN_SCOPED");

  scopedCapability.capability.inputSchema = JSON.stringify({
    type: "object",
    properties: { another: { type: "string" } },
  });
  loaders.capabilities.mockResolvedValue({
    items: [scopedCapability],
    total: 1,
  });
  await state.loadCapabilities("", undefined, new AbortController().signal);
  state.chooseCapability({ ref: "read", title: "Чтение" });
  expect(state.approvalScopePaths.value).toEqual([]);
});

it("явно выбирает catalog default и отправляет выбранную allowed policy вместо implicit fallback", async () => {
  const candidate: IntegrationGrantCapabilityCandidate = {
    ...capability,
    capability: {
      ...capability.capability,
      risk: "WRITE",
      approvalRequired: true,
      approvalPolicy: "HUMAN_EACH_EFFECT",
      allowedApprovalPolicies: ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
    },
  };
  const { state, emit } = await panel();
  loaders.capabilities.mockResolvedValue({ items: [candidate], total: 1 });
  await state.loadCapabilities("", undefined, new AbortController().signal);
  state.chooseCapability({ ref: "read", title: "Действие" });
  expect(state.selectedApprovalPolicy.value).toBe("HUMAN_EACH_EFFECT");
  state.submit();
  expect(emit).toHaveBeenCalledWith(
    "save",
    expect.objectContaining({ approvalPolicy: "HUMAN_EACH_EFFECT" }),
  );
  state.changeApprovalPolicy("NONE");
  emit.mockClear();
  state.submit();
  expect(emit).toHaveBeenCalledWith(
    "save",
    expect.objectContaining({ approvalPolicy: "NONE" }),
  );
  expect(state.availableApprovalPolicies.value).toEqual(
    candidate.capability.allowedApprovalPolicies,
  );
  state.changeApprovalPolicy("UNKNOWN");
  emit.mockClear();
  state.submit();
  expect(emit.mock.calls.some(([name]) => name === "save")).toBe(false);
});

it("не заменяет более недоступную current grant policy каталоговым default без выбора владельца", async () => {
  const selectedConnection: IntegrationConnection = {
    ...connection,
    grants: [
      {
        ref: "grant_previous",
        version: 1,
        capabilityKey: "read",
        agentRef: "agent",
        targetName: "Агент",
        enabled: true,
        risk: "READ",
        approvalPolicy: "NONE",
        resourceScope: {
          kind: "GITHUB_REPOSITORY",
          values: { repository: "example" },
          digest: "c".repeat(64),
        },
      },
    ],
  };
  const candidate: IntegrationGrantCapabilityCandidate = {
    ...capability,
    currentGrantRef: "grant_previous",
    currentGrantVersion: 1,
    capability: {
      ...capability.capability,
      risk: "WRITE",
      approvalRequired: true,
      approvalPolicy: "HUMAN_EACH_EFFECT",
      allowedApprovalPolicies: ["HUMAN_EACH_EFFECT"],
    },
  };
  const { state, emit } = await panel(selectedConnection);
  loaders.capabilities.mockResolvedValue({ items: [candidate], total: 1 });
  await state.loadCapabilities("", undefined, new AbortController().signal);
  state.chooseCapability({ ref: "read", title: "Действие" });
  expect(state.selectedApprovalPolicy.value).toBeUndefined();
  emit.mockClear();
  state.submit();
  expect(emit.mock.calls.some(([name]) => name === "save")).toBe(false);
  state.changeApprovalPolicy("HUMAN_EACH_EFFECT");
  state.submit();
  expect(emit).toHaveBeenCalledWith(
    "save",
    expect.objectContaining({ approvalPolicy: "HUMAN_EACH_EFFECT" }),
  );
});

it("Human Scoped требует явную область и смена policy отзывает прежний выбор параметров", async () => {
  const { state, emit } = await panel();
  state.capabilityCandidate.value = {
    ...capability,
    capability: {
      ...capability.capability,
      risk: "WRITE",
      approvalRequired: true,
      approvalPolicy: "HUMAN_EACH_EFFECT",
      allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
      inputSchema: JSON.stringify({
        type: "object",
        properties: { ref: { type: "string" } },
      }),
    },
  };
  state.changeApprovalPolicy("HUMAN_SCOPED");
  emit.mockClear();
  state.submit();
  expect(emit.mock.calls.some(([name]) => name === "save")).toBe(false);
  state.approvalScopePaths.value = ["/ref"];
  state.submit();
  expect(emit).toHaveBeenCalledWith(
    "save",
    expect.objectContaining({
      approvalPolicy: "HUMAN_SCOPED",
      approvalScopePaths: ["/ref"],
    }),
  );
  state.changeApprovalPolicy("HUMAN_EACH_EFFECT");
  expect(state.approvalScopePaths.value).toEqual([]);
  emit.mockClear();
  state.submit();
  const saved = emit.mock.calls.find(([name]) => name === "save");
  expect(saved?.[1]).not.toHaveProperty("approvalScopePaths");
});
