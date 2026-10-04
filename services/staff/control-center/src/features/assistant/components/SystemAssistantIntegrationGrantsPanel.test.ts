import { createI18n } from "vue-i18n";
import { nextTick, reactive, type ComputedRef, type Ref } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type {
  IntegrationConnection,
  SystemAssistant,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
const api = vi.hoisted(() => ({
  connections: vi.fn(),
  candidates: vi.fn(),
  save: vi.fn(),
  owner: new AbortController(),
}));
vi.mock("@/shared/api/owner-lifetime", () => ({
  ownerRequestSignal: () => api.owner.signal,
}));
vi.mock("@/features/assistant/system-integration-grants", async (original) => ({
  ...(await original<object>()),
  readSystemGrantConnections: api.connections,
  readSystemGrantCandidates: api.candidates,
  saveSystemGrant: api.save,
}));
const platform = reactive({
  bootstrap: { organizationRef: "organization_test" },
});
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => platform,
}));
import Panel from "./SystemAssistantIntegrationGrantsPanel.vue";

const connection: IntegrationConnection = {
  ref: "connection_test",
  version: 5,
  definitionKey: "github",
  definitionVersion: "1.0",
  definitionDigest: "a".repeat(64),
  name: "GitHub",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: [],
  publicConfiguration: {},
};
const candidate: SystemAssistantIntegrationGrantCandidate = {
  capability: {
    key: "create",
    name: "Создать",
    description: "",
    risk: "WRITE",
    approvalRequired: true,
    operation: "CREATE",
    resourceKind: "GITHUB_REPOSITORY",
    inputFields: [],
    approvalPolicy: "HUMAN_EACH_EFFECT",
    allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
    inputSchema:
      '{"type":"object","properties":{"repository":{"type":"string"}}}',
  },
  grantable: true,
  reason: "READY",
  currentGrantVersion: 0,
  currentGrantEnabled: false,
  currentApprovalScopePaths: [],
};
interface State {
  connection: Ref<IntegrationConnection | undefined>;
  candidate: Ref<SystemAssistantIntegrationGrantCandidate | undefined>;
  policy: Ref<string | undefined>;
  paths: Ref<string[]>;
  canSave: ComputedRef<boolean>;
  busy: Ref<boolean>;
  loadConnections(
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ): Promise<AsyncEntityOptionPage>;
  loadCapabilities(
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
  ): Promise<AsyncEntityOptionPage>;
  chooseConnection(option: AsyncEntityOption): void;
  chooseCapability(option: AsyncEntityOption): void;
  changePolicy(value: string): void;
  clearConnection(): void;
  save(): Promise<void>;
}
async function setup(canEdit = true): Promise<State> {
  return (await captureSetupState(
    Panel,
    (app) =>
      app.use(
        createI18n({
          legacy: false,
          locale: "ru",
          missingWarn: false,
          fallbackWarn: false,
        }),
      ),
    {
      assistant: { ref: "assistant_test", version: 3 } as SystemAssistant,
      canEdit,
    },
  )) as unknown as State;
}
async function select(state: State): Promise<void> {
  await state.loadConnections("", undefined, new AbortController().signal);
  state.chooseConnection({ ref: connection.ref, title: connection.name });
  await state.loadCapabilities("", undefined, new AbortController().signal);
  state.chooseCapability({
    ref: candidate.capability.key,
    title: candidate.capability.name,
  });
}
describe("SYSTEM grant controls", () => {
  beforeEach(() => {
    api.owner.abort();
    api.owner = new AbortController();
    vi.clearAllMocks();
    platform.bootstrap.organizationRef = "organization_test";
    api.connections.mockResolvedValue({
      items: [connection],
      nextPageToken: "",
    });
    api.candidates.mockResolvedValue({
      items: [candidate],
      total: 1,
      nextPageToken: "",
    });
  });
  it("явно предвыбирает каталог policy и отправляет только specialized fields", async () => {
    const state = await setup();
    await select(state);
    expect(state.policy.value).toBe("HUMAN_EACH_EFFECT");
    expect(state.canSave.value).toBe(true);
    api.save.mockResolvedValue({ ...connection, version: 6 });
    await state.save();
    expect(api.save).toHaveBeenCalledWith(
      connection,
      {
        connectionRef: connection.ref,
        capabilityKey: candidate.capability.key,
        enabled: true,
        approvalPolicy: "HUMAN_EACH_EFFECT",
      },
      expect.any(AbortSignal),
    );
    expect(state.candidate.value).toBeUndefined();
    expect(state.connection.value?.version).toBe(6);
  });
  it("блокирует неизвестную policy, требует bounded scope и presentation EDIT", async () => {
    const state = await setup();
    await select(state);
    state.changePolicy("NONE");
    expect(state.canSave.value).toBe(false);
    state.changePolicy("HUMAN_SCOPED");
    expect(state.canSave.value).toBe(false);
    state.paths.value = ["/repository"];
    expect(state.canSave.value).toBe(true);
    const readonly = await setup(false);
    await select(readonly);
    expect(readonly.canSave.value).toBe(false);
  });
  it("не возвращает позднее чтение прежнего подключения после очистки", async () => {
    const state = await setup();
    await select(state);
    let resolve!: (value: {
      items: SystemAssistantIntegrationGrantCandidate[];
      total: number;
      nextPageToken: string;
    }) => void;
    api.candidates.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const reading = state.loadCapabilities(
      "",
      undefined,
      new AbortController().signal,
    );
    state.clearConnection();
    resolve({ items: [candidate], total: 1, nextPageToken: "" });
    await expect(reading).rejects.toMatchObject({ name: "AbortError" });
    expect(state.connection.value).toBeUndefined();
    expect(state.candidate.value).toBeUndefined();
  });
  it("сразу очищает выбор после owner invalidation и не принимает позднюю mutation", async () => {
    const state = await setup();
    await select(state);
    let resolve!: (value: IntegrationConnection) => void;
    api.save.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const saving = state.save();
    api.owner.abort();
    resolve({ ...connection, version: 6 });
    await saving;
    await nextTick();
    expect(state.connection.value).toBeUndefined();
    expect(state.candidate.value).toBeUndefined();
    expect(state.canSave.value).toBe(false);
    expect(state.busy.value).toBe(false);
  });
});
