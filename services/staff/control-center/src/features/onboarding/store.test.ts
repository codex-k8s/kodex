import { createPinia, setActivePinia } from "pinia";
import { nextTick, reactive } from "vue";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const dependencies = vi.hoisted(() => ({
  platform: {} as Record<string, unknown>,
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => dependencies.platform,
}));
vi.mock("@/features/providers/store", () => ({
  useProvidersStore: () => ({ snapshotAccounts: [] }),
}));
vi.mock("@/features/runtime/store", () => ({
  useRuntimeStore: () => ({ environments: {} }),
}));
import { useOnboardingStore } from "./store";

let storage: Map<string, string>;
function changeOwner(ref: string): void {
  dependencies.platform.bootstrap = {
    currentUser: { ref },
    nextActions: [],
    onboardingComplete: false,
  };
}
beforeEach(() => {
  setActivePinia(createPinia());
  dependencies.platform = reactive({
    bootstrap: undefined,
    projectList: [],
    roleImageRecipes: {},
    agents: {},
    workflows: {},
    runs: {},
  });
  storage = new Map();
  vi.stubGlobal("window", {
    sessionStorage: {
      getItem: (key: string) => storage.get(key) ?? null,
      setItem: (key: string, value: string) => storage.set(key, value),
    },
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("навигация первичной настройки", () => {
  it("сохраняет этап и выбранный проект отдельно для текущего пользователя", () => {
    changeOwner("owner-one");
    const guide = useOnboardingStore();
    guide.selectProject("prj_first");
    guide.selectStep("environment");
    expect(
      JSON.parse(storage.get("kodex:onboarding:owner-one") ?? "null"),
    ).toEqual({ step: "environment", projectRef: "prj_first" });
  });
  it("после загрузки bootstrap восстанавливает сохранённый этап без перезаписи", async () => {
    storage.set(
      "kodex:onboarding:owner-one",
      JSON.stringify({ step: "team", projectRef: "prj_saved" }),
    );
    const guide = useOnboardingStore();
    changeOwner("owner-one");
    await nextTick();
    expect(guide.step).toBe("team");
    expect(guide.projectRef).toBe("prj_saved");
    expect(guide.returnTo.query.step).toBe("team");
  });
  it("при смене пользователя не переносит чужую навигацию и не портит новый snapshot", async () => {
    changeOwner("owner-one");
    const guide = useOnboardingStore();
    guide.selectProject("prj_private");
    guide.selectStep("launch");
    const saved = JSON.stringify({ step: "image", projectRef: "prj_other" });
    storage.set("kodex:onboarding:owner-two", saved);
    changeOwner("owner-two");
    await nextTick();
    expect(guide.step).toBe("image");
    expect(guide.projectRef).toBe("prj_other");
    expect(storage.get("kodex:onboarding:owner-two")).toBe(saved);
  });
  it("недоступное хранилище не блокирует ручную настройку", () => {
    vi.stubGlobal("window", {
      get sessionStorage() {
        throw new Error("Storage unavailable");
      },
    });
    changeOwner("owner-one");
    const guide = useOnboardingStore();
    expect(() => guide.selectStep("project")).not.toThrow();
    expect(guide.step).toBe("project");
  });
});
