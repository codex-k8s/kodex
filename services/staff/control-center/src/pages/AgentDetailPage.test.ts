import { ref, type Ref } from "vue";
import { beforeEach, expect, it, vi } from "vitest";
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
  useRoute: () => routeState,
  useRouter: () => navigation,
}));
vi.mock("vue-i18n", () => ({
  useI18n: () => ({ locale: ref("ru"), t: (key: string) => key }),
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => platform,
}));
vi.mock("@/shared/ui/unsaved-changes", () => ({ useUnsavedChanges: vi.fn() }));
import AgentDetailPage from "./AgentDetailPage.vue";

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
