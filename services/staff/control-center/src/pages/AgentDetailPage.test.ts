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
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
const navigation = vi.hoisted(() => ({ replace: vi.fn() }));
const routeState = vi.hoisted(() => ({
  query: { tab: "runtime" },
  params: { agentRef: "agent_one", projectRef: "project_one" },
}));
const platform = vi.hoisted(() => ({
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
import AgentDetailPage from "./AgentDetailPage.vue";

const currentRoute = reactive(routeState);
const currentPlatform = reactive(platform);
const unmounts: Array<() => void> = [];
afterEach(() => {
  for (const unmount of unmounts.splice(0)) unmount();
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
