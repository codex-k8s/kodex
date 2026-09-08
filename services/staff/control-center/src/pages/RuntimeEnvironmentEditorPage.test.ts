import { beforeEach, describe, expect, it, vi } from "vitest";
import { reactive, type Ref } from "vue";
import { createI18n } from "vue-i18n";
import { captureSetupState } from "@/test-utils/setup-harness";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";

const runtime = vi.hoisted(() => ({
  environments: {},
  environmentVersions: {},
  environmentReadiness: {},
  environmentAgents: {},
  loadPromotedRoleImageArtifact: vi.fn(),
  loadEnvironment: vi.fn(),
  loadEnvironmentVersions: vi.fn(),
}));
const cleanup = vi.hoisted(() => [] as Array<() => void>);
vi.mock("vue", async (original) => ({
  ...(await original<typeof import("vue")>()),
  onBeforeUnmount: (callback: () => void) => cleanup.push(callback),
}));
const route = reactive({
  params: { projectRef: "project_1", environmentRef: "environment_1" },
  query: {},
});
vi.mock("vue-router", () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  onBeforeRouteLeave: vi.fn(),
  onBeforeRouteUpdate: vi.fn(),
}));
vi.mock("@/features/runtime/store", () => ({ useRuntimeStore: () => runtime }));
vi.mock("@/features/session/store", () => ({ useSessionStore: () => ({}) }));
import RuntimeEnvironmentEditorPage from "./RuntimeEnvironmentEditorPage.vue";

async function editor() {
  return (await captureSetupState(RuntimeEnvironmentEditorPage, (app) =>
    app.use(createI18n({ legacy: false, locale: "ru" })),
  )) as unknown as {
    input: { imageArtifactRef: string; name: string };
    imageArtifact: Ref<RoleImageArtifact | undefined>;
    imageLoading: Ref<boolean>;
    imageProblem: Ref<unknown>;
    loadImageArtifact(recipe: string, artifact: string): Promise<void>;
    load(): Promise<void>;
  };
}
function pending<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((accept, refuse) => {
    resolve = accept;
    reject = refuse;
  });
  return { promise, resolve, reject };
}
beforeEach(() => {
  vi.clearAllMocks();
  cleanup.length = 0;
  route.params.projectRef = "project_1";
  route.params.environmentRef = "environment_1";
});

describe("ответы образа принадлежат текущему окружению", () => {
  it.each(["success", "failure"])(
    "старый %s не заменяет новый образ",
    async (outcome) => {
      const state = await editor();
      const first = pending<RoleImageArtifact>();
      const second = pending<RoleImageArtifact>();
      runtime.loadPromotedRoleImageArtifact
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise);
      state.input.imageArtifactRef = "image_1";
      const firstRead = state.loadImageArtifact("recipe_1", "image_1");
      const firstSignal = runtime.loadPromotedRoleImageArtifact.mock
        .calls[0]?.[3] as AbortSignal;
      state.input.imageArtifactRef = "image_2";
      const secondRead = state.loadImageArtifact("recipe_2", "image_2");
      expect(firstSignal.aborted).toBe(true);
      if (outcome === "success")
        first.resolve({ ref: "image_1" } as RoleImageArtifact);
      else first.reject(new Error("stale image failure"));
      await firstRead;
      expect(state.imageLoading.value).toBe(true);
      expect(state.imageArtifact.value).toBeUndefined();
      expect(state.imageProblem.value).toBeUndefined();
      second.resolve({ ref: "image_2" } as RoleImageArtifact);
      await secondRead;
      expect(state.imageArtifact.value?.ref).toBe("image_2");
      expect(state.imageLoading.value).toBe(false);
    },
  );

  it("не принимает image readback другого проекта", async () => {
    const state = await editor();
    const request = pending<RoleImageArtifact>();
    runtime.loadPromotedRoleImageArtifact.mockReturnValueOnce(request.promise);
    state.input.imageArtifactRef = "image_1";
    const loading = state.loadImageArtifact("recipe_1", "image_1");
    route.params.projectRef = "project_2";
    request.resolve({ ref: "image_1" } as RoleImageArtifact);
    await loading;
    expect(state.imageArtifact.value).toBeUndefined();
  });

  it("закрытие редактора отменяет запрос и отбрасывает позднюю ошибку", async () => {
    vi.stubGlobal("window", { removeEventListener: vi.fn() });
    try {
      const state = await editor();
      const request = pending<RoleImageArtifact>();
      runtime.loadPromotedRoleImageArtifact.mockReturnValueOnce(
        request.promise,
      );
      state.input.imageArtifactRef = "image_1";
      const loading = state.loadImageArtifact("recipe_1", "image_1");
      const signal = runtime.loadPromotedRoleImageArtifact.mock
        .calls[0]?.[3] as AbortSignal;
      for (const callback of cleanup) callback();
      expect(signal.aborted).toBe(true);
      request.reject(new Error("closed image failure"));
      await loading;
      expect(state.imageProblem.value).toBeUndefined();
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("старый route load не синхронизирует форму нового окружения", async () => {
    const state = await editor();
    const request = pending<undefined>();
    runtime.loadEnvironment.mockReturnValueOnce(request.promise);
    runtime.loadEnvironmentVersions.mockResolvedValueOnce(undefined);
    const loading = state.load();
    Object.assign(runtime.environments, {
      environment_2: { name: "Сохранённое имя другого окружения" },
    });
    route.params.environmentRef = "environment_2";
    state.input.name = "Новый ввод";
    request.resolve(undefined);
    await loading;
    expect(state.input.name).toBe("Новый ввод");
    expect(runtime.loadPromotedRoleImageArtifact).not.toHaveBeenCalled();
  });
});
