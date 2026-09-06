import { defineComponent, type Ref, type SetupContext } from "vue";
import { createI18n } from "vue-i18n";
import { beforeEach, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type {
  IntegrationConnection,
  IntegrationGrantProjectCandidate,
  IntegrationGrantRecipientCandidate,
  IntegrationGrantCapabilityCandidate,
} from "@/shared/api/generated/openapi/types.gen";

const loaders = vi.hoisted(() => ({ projects: vi.fn() }));
vi.mock("@/features/integrations/grant-candidates", () => ({
  connectionCandidates: () => vi.fn(),
  projectCandidates: () => loaders.projects,
  recipientCandidates: () => vi.fn(),
  capabilityCandidates: () => vi.fn(),
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
  projectCandidate: Ref<IntegrationGrantProjectCandidate | undefined>;
  recipientCandidate: Ref<IntegrationGrantRecipientCandidate | undefined>;
  capabilityCandidate: Ref<IntegrationGrantCapabilityCandidate | undefined>;
}
async function panel() {
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
            selectedConnection: connection,
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
  return { state: result, emit };
}
beforeEach(() => vi.resetAllMocks());
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
