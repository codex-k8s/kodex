import { beforeEach, expect, it, vi } from "vitest";
import type { Ref } from "vue";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";

const lifecycle = vi.hoisted(() => ({
  mounted: [] as Array<() => void>,
  cleanup: [] as Array<() => void>,
}));
const runtime = vi.hoisted(() => ({
  environmentReadiness: {},
  environmentAgents: {},
  problems: {},
  searchEnvironmentPage: vi.fn(),
  loadEnvironmentReadiness: vi.fn(),
  loadEnvironmentAgents: vi.fn(),
}));
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onMounted: (callback: () => void) => lifecycle.mounted.push(callback),
  onBeforeUnmount: (callback: () => void) => lifecycle.cleanup.push(callback),
}));
vi.mock("vue-router", () => ({
  useRoute: () => ({ params: { projectRef: "project_1" } }),
  useRouter: () => ({ push: vi.fn() }),
}));
vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => runtime }));
import RuntimeEnvironmentsPage from "./RuntimeEnvironmentsPage.vue";

async function catalog() {
  return (await captureSetupState(RuntimeEnvironmentsPage, (app) =>
    app.use(createI18n({ legacy: false, locale: "ru" })),
  )) as unknown as {
    selectedRef: Ref<string>;
    dismissInspectorWithKeyboard(event: KeyboardEvent): void;
  };
}
beforeEach(() => {
  vi.clearAllMocks();
  lifecycle.mounted.length = 0;
  lifecycle.cleanup.length = 0;
  runtime.searchEnvironmentPage.mockResolvedValue({ items: [] });
});

it("каталог не выбирает окружение и не загружает inspector заранее", async () => {
  const state = await catalog();
  expect(state.selectedRef.value).toBe("");
  expect(runtime.loadEnvironmentReadiness).not.toHaveBeenCalled();
  expect(runtime.loadEnvironmentAgents).not.toHaveBeenCalled();
});

it("Escape с фокусом вне таблицы закрывает inspector, listener удаляется", async () => {
  const documentTarget = new EventTarget();
  vi.stubGlobal("document", documentTarget);
  try {
    const state = await catalog();
    for (const callback of lifecycle.mounted) callback();
    state.selectedRef.value = "environment_1";
    documentTarget.dispatchEvent(
      Object.assign(new Event("keydown"), { key: "Escape" }),
    );
    expect(state.selectedRef.value).toBe("");
    for (const callback of lifecycle.cleanup) callback();
    state.selectedRef.value = "environment_2";
    documentTarget.dispatchEvent(
      Object.assign(new Event("keydown"), { key: "Escape" }),
    );
    expect(state.selectedRef.value).toBe("environment_2");
  } finally {
    vi.unstubAllGlobals();
  }
});

it("обработанный дочерним контролом Escape не закрывает inspector", async () => {
  const state = await catalog();
  state.selectedRef.value = "environment_1";
  state.dismissInspectorWithKeyboard({
    key: "Escape",
    defaultPrevented: true,
  } as KeyboardEvent);
  state.dismissInspectorWithKeyboard({
    key: "Enter",
    defaultPrevented: false,
  } as KeyboardEvent);
  expect(state.selectedRef.value).toBe("environment_1");
});
