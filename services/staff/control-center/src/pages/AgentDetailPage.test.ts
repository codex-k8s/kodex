import {
  createRenderer,
  defineComponent,
  h,
  nextTick,
  reactive,
  ref,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { captureSetupState } from "@/test-utils/setup-harness";
import type {
  Agent,
  RevisionImpactPlan,
} from "@/shared/api/generated/openapi/types.gen";
import { rememberPublicationAttempt } from "@/features/runtime/publication-attempt";
import { AppProblem } from "@/shared/api/problem";
const publicationApi = vi.hoisted(() => ({
  getRevisionImpactPlan: vi.fn(),
  readInstructionPublicationAgent: vi.fn(),
  publishInstructions: vi.fn(),
}));
const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
const routeState = vi.hoisted(() => ({
  query: { tab: "runtime" },
  params: { agentRef: "agent_one", projectRef: "project_one" },
}));
const platform = vi.hoisted(() => ({
  bootstrap: { organizationRef: "org_synthetic" } as
    | { organizationRef: string }
    | undefined,
  problems: {} as Record<string, AppProblem | undefined>,
  agents: {} as Record<string, Agent>,
  loadProject: vi.fn(),
  loadAgent: vi.fn(),
  loadInstructionVersions: vi.fn(),
  loadCapabilities: vi.fn(),
  saveInstructions: vi.fn(),
}));
vi.mock("vue-router", () => ({
  onBeforeRouteUpdate: vi.fn(),
  useRoute: () => currentRoute,
  useRouter: () => navigation,
}));
vi.mock("vue-i18n", () => ({
  useI18n: () => ({ locale: ref("ru"), t: (key: string) => key }),
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => currentPlatform,
}));
vi.mock("@/shared/ui/unsaved-changes", () => ({ useUnsavedChanges: vi.fn() }));
vi.mock("@/shared/config/runtime", () => ({
  runtimeConfig: () => ({ requestTimeoutMs: 1000 }),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", async (original) => ({
  ...(await original<
    typeof import("@/shared/api/generated/openapi/sdk.gen")
  >()),
  getRevisionImpactPlan: publicationApi.getRevisionImpactPlan,
}));
vi.mock(
  "@/features/agents/detail/instruction-publication",
  async (original) => ({
    ...(await original<
      typeof import("@/features/agents/detail/instruction-publication")
    >()),
    readInstructionPublicationAgent:
      publicationApi.readInstructionPublicationAgent,
    publishInstructions: publicationApi.publishInstructions,
  }),
);
import AgentDetailPage from "./AgentDetailPage.vue";

const currentRoute = reactive(routeState);
const currentPlatform = reactive(platform);
const unmounts: Array<() => void> = [];
afterEach(() => {
  for (const unmount of unmounts.splice(0)) unmount();
  vi.unstubAllGlobals();
});
type EditorState = {
  load(): Promise<void>;
  instructions: Ref<string>;
  instructionsDirty: Ref<boolean>;
  agent: Ref<Agent | undefined>;
  loaded: Ref<boolean>;
};
function mountEditor(): EditorState {
  let state!: EditorState;
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
    patchProp() {},
  });
  const component = defineComponent({
    setup(props, context) {
      state = (
        AgentDetailPage as unknown as {
          setup(props: object, context: SetupContext): EditorState;
        }
      ).setup(props, context);
      return () => h("div");
    },
  });
  const app = renderer.createApp(component);
  app.provide(ssrContextKey, {});
  app.mount({});
  unmounts.push(() => app.unmount());
  return state;
}
function agentSnapshot(version = 1, draft?: string): Agent {
  return {
    ref: "agent_one",
    projectRef: "project_one",
    name: "Сотрудник",
    purpose: "Задача",
    roleDescription: "Описание",
    version,
    nextActions: draft ? ["EDIT", "VALIDATE"] : ["EDIT"],
    publishedInstructions: {
      content: "Опубликованные инструкции",
      state: "PUBLISHED",
    },
    ...(draft ? { draftInstructions: { content: draft, state: "DRAFT" } } : {}),
  } as Agent;
}

it("принимает новый черновик из realtime в чистую форму без повторного чтения", async () => {
  currentPlatform.agents.agent_one = agentSnapshot();
  const state = mountEditor();
  await state.load();
  const reads = platform.loadAgent.mock.calls.length;
  expect(state.instructionsDirty.value).toBe(false);
  currentPlatform.agents.agent_one = agentSnapshot(
    2,
    "Новый серверный черновик",
  );
  await nextTick();
  expect(state.instructions.value).toBe("Новый серверный черновик");
  expect(state.instructionsDirty.value).toBe(false);
  expect(state.agent.value?.nextActions).toContain("VALIDATE");
  expect(platform.loadAgent).toHaveBeenCalledTimes(reads);
});

it("не перезаписывает несохранённые инструкции новым серверным черновиком", async () => {
  currentPlatform.agents.agent_one = agentSnapshot();
  const state = mountEditor();
  await state.load();
  state.instructions.value = "Локальный ввод";
  currentPlatform.agents.agent_one = agentSnapshot(
    2,
    "Новый серверный черновик",
  );
  await nextTick();
  expect(state.instructions.value).toBe("Локальный ввод");
  expect(state.instructionsDirty.value).toBe(true);
});

it("не принимает инструкции другого проекта и очищает форму при утрате доступного сотрудника", async () => {
  currentPlatform.agents.agent_one = agentSnapshot();
  const state = mountEditor();
  await state.load();
  state.instructions.value = "Локальный ввод";
  currentPlatform.agents.agent_one = {
    ...agentSnapshot(2, "Чужие инструкции"),
    projectRef: "project_other",
  };
  await nextTick();
  expect(state.agent.value).toBeUndefined();
  expect(state.instructions.value).toBe("");
  currentPlatform.agents.agent_one = agentSnapshot(3, "Доступный черновик");
  await nextTick();
  expect(state.instructions.value).toBe("Доступный черновик");
  Reflect.deleteProperty(currentPlatform.agents, "agent_one");
  await nextTick();
  expect(state.instructions.value).toBe("");
});

it("не переносит поздний snapshot инструкций в другой route-контекст", async () => {
  currentPlatform.agents.agent_one = agentSnapshot();
  const state = mountEditor();
  await state.load();
  currentRoute.params = { agentRef: "agent_two", projectRef: "project_two" };
  currentPlatform.agents.agent_one = agentSnapshot(
    2,
    "Поздний старый черновик",
  );
  await nextTick();
  expect(state.agent.value).toBeUndefined();
  expect(state.instructions.value).toBe("");
});

beforeEach(() => {
  for (const mock of Object.values(publicationApi)) mock.mockReset();
  currentPlatform.bootstrap = { organizationRef: "org_synthetic" };
  currentPlatform.problems = {};
  navigation.replace.mockReset();
  routeState.query = { tab: "runtime" };
  routeState.params = { agentRef: "agent_one", projectRef: "project_one" };
  for (const ref of Object.keys(platform.agents))
    Reflect.deleteProperty(platform.agents, ref);
  platform.loadProject.mockReset().mockResolvedValue(undefined);
  platform.loadAgent.mockReset().mockResolvedValue(undefined);
  platform.loadInstructionVersions.mockReset().mockResolvedValue(undefined);
  platform.loadCapabilities.mockReset().mockResolvedValue(undefined);
  platform.saveInstructions.mockReset();
});

it("перенаправляет сотрудника в его авторитетный Проект", async () => {
  platform.loadAgent.mockImplementation(() => {
    platform.agents.agent_one = {
      ref: "agent_one",
      projectRef: "project/two",
      nextActions: ["LAUNCH"],
    } as Agent;
    return Promise.resolve();
  });
  const state = (await captureSetupState(AgentDetailPage)) as unknown as {
    load(): Promise<void>;
    loaded: Ref<boolean>;
    agent: Ref<Agent | undefined>;
  };

  await state.load();

  expect(navigation.replace).toHaveBeenCalledWith({
    path: "/projects/project%2Ftwo/agents/agent_one",
    query: { tab: "runtime" },
  });
  expect(state.loaded.value).toBe(false);
  expect(state.agent.value).toBeUndefined();
});

it("сохраняет runtime-вкладку, пока route guard не разрешил переход", async () => {
  navigation.replace.mockResolvedValue({ type: 4 });
  const state = (await captureSetupState(AgentDetailPage)) as unknown as {
    selectTab(tab: "profile"): void;
    activeTab: Ref<string>;
  };
  state.selectTab("profile");
  await Promise.resolve();
  expect(navigation.replace).toHaveBeenCalledWith({
    query: { tab: "profile" },
  });
  expect(state.activeTab.value).toBe("runtime");
});

it("не меняет новый редактор поздним ответом сохранения инструкций", async () => {
  platform.agents.agent_one = {
    ref: "agent_one",
    projectRef: "project_one",
    nextActions: ["EDIT"],
    publishedInstructions: { content: "original" },
  } as Agent;
  let finish!: (value: Agent) => void;
  platform.saveInstructions.mockImplementation(
    () =>
      new Promise<Agent>((resolve) => {
        finish = resolve;
      }),
  );
  const state = (await captureSetupState(AgentDetailPage)) as unknown as {
    instructions: Ref<string>;
    busy: Ref<boolean>;
    resetContext(): void;
    saveInstructions(): Promise<void>;
    updateInstructions(value: string): void;
  };
  state.instructions.value = "Первый текст";
  const saving = state.saveInstructions();
  state.updateInstructions("Поздний ввод");
  await state.saveInstructions();
  expect(state.instructions.value).toBe("Первый текст");
  expect(platform.saveInstructions).toHaveBeenCalledOnce();
  state.resetContext();
  state.instructions.value = "Новый контекст";
  finish({
    ref: "agent_one",
    publishedInstructions: { content: "Старый ответ" },
  } as Agent);
  await saving;
  expect(state.instructions.value).toBe("Новый контекст");
  expect(state.busy.value).toBe(false);
});

it.each(["recover", "retry", "stored"] as const)(
  "instruction %s передаёт trusted organization impact anchor",
  async (operation) => {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() {
        return values.size;
      },
      key: (index) => [...values.keys()][index] ?? null,
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => {
        values.set(key, value);
      },
      removeItem: (key) => {
        values.delete(key);
      },
      clear: () => values.clear(),
    };
    vi.stubGlobal("window", { sessionStorage: storage });
    const agent = {
      ...agentSnapshot(),
      nextActions: ["EDIT", "PUBLISH"],
      draftInstructions: {
        ref: "draft_synthetic",
        version: 1,
        content: "Опубликованные инструкции",
        state: "VALID",
      },
    } as Agent;
    currentPlatform.agents.agent_one = agent;
    const plan: RevisionImpactPlan = {
      ref: "plan_synthetic",
      version: 1,
      kind: "AGENT_INSTRUCTIONS",
      sourceRef: agent.ref,
      sourceVersion: agent.version,
      draftRef: "draft_synthetic",
      draftVersion: 1,
      targetDigest: "d".repeat(64),
      digest: "e".repeat(64),
      total: 0,
      state: "PREPARED",
      createdAt: "2026-09-06T00:00:00Z",
      expiresAt: "2099-09-06T00:00:00Z",
    };
    const attempt = {
      kind: "AGENT_INSTRUCTIONS" as const,
      ownerRef: agent.ref,
      planRef: plan.ref,
      version: agent.version,
      selectedItemRefs: [],
      key: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
    };
    const state = (await captureSetupState(AgentDetailPage)) as unknown as {
      instructionPlan: Ref<RevisionImpactPlan | undefined>;
      instructionAttempt: Ref<typeof attempt | undefined>;
      problem: Ref<AppProblem | undefined>;
      instructionsDirty: Ref<boolean>;
      load(): Promise<void>;
      recoverInstructionPublication(): Promise<void>;
      retryInstructionPublication(): Promise<void>;
      instructionAction(action: "PUBLISH"): Promise<void>;
    };
    await state.load();
    expect(state.instructionsDirty.value).toBe(false);
    state.instructionPlan.value = plan;
    state.instructionAttempt.value = attempt;
    const applied = {
      ...plan,
      version: 2,
      state: "APPLIED" as const,
      publishedRevisionRef: "published_synthetic",
    };
    publicationApi.getRevisionImpactPlan.mockResolvedValueOnce({
      data: {
        plan: operation === "retry" ? plan : applied,
        total: 0,
        items: [],
      },
      response: new Response(null, { status: 200 }),
    });
    if (operation === "stored") {
      rememberPublicationAttempt(attempt, storage);
      await state.instructionAction("PUBLISH");
    } else if (operation === "recover")
      await state.recoverInstructionPublication();
    else {
      publicationApi.readInstructionPublicationAgent.mockResolvedValueOnce(
        agent,
      );
      publicationApi.publishInstructions.mockRejectedValueOnce(
        new AppProblem({
          status: 503,
          code: "SERVICE_UNAVAILABLE",
          retryable: true,
          kind: "unavailable",
        }),
      );
      await state.retryInstructionPublication();
      expect(publicationApi.publishInstructions).toHaveBeenCalledOnce();
      expect(publicationApi.publishInstructions.mock.calls[0]?.[3]).toBe(
        attempt.key,
      );
      return;
    }
    expect(state.problem.value).toBeUndefined();
    expect(state.instructionPlan.value).toMatchObject({ state: "APPLIED" });
    expect(publicationApi.publishInstructions).not.toHaveBeenCalled();
  },
);
