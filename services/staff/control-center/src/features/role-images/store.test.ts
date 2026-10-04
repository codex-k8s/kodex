import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  Agent,
  RoleEnvironment,
  RoleImageRecipe,
  RoleImageRecipePage,
} from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({
  loadRoleImagePage: vi.fn(),
  loadRoleImageDetail: vi.fn(),
  loadRoleImageRevisionPage: vi.fn(),
}));
vi.mock("./api", () => api);
import { useRoleImagesStore } from "./store";
import { usePlatformStore } from "@/features/platform/store";
import type { BootstrapState } from "@/shared/api/generated/openapi/types.gen";

const recipe: RoleImageRecipe = {
  scopeKind: "PROJECT",
  organizationRef: "org_synthetic",
  sourceAvailable: true,
  ref: "image_synthetic",
  projectRef: "project_synthetic",
  version: 3,
  roleDefinitionRef: "role_synthetic",
  name: "Среда аналитика",
  state: "ACTIVE",
  environment: { environmentKey: "standard", dockerfile: "FROM scratch" },
  generation: 3,
  promotedImageReady: false,
  nextActions: ["OPEN"],
  createdAt: "2026-09-05T00:00:00Z",
  updatedAt: "2026-09-05T00:00:00Z",
};
describe("role image catalog store", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    usePlatformStore().bootstrap = {
      organizationRef: "org_synthetic",
    } as BootstrapState;
    vi.resetAllMocks();
  });
  it("закрывает PROJECT snapshot без bootstrap org вместо доверия owner из DTO", () => {
    usePlatformStore().bootstrap = undefined;
    const store = useRoleImagesStore();
    expect(() =>
      store.applyCatalogSnapshot(recipe.projectRef, [recipe]),
    ).toThrow("anchor");
    expect(store.recipes[recipe.ref]).toBeUndefined();
    expect(store.catalog(recipe.projectRef)).toEqual([]);
  });
  it("сверяет PROJECT snapshot с bootstrap org и скрывает прежний owner cache", () => {
    const platform = usePlatformStore();
    platform.bootstrap = { organizationRef: "org_synthetic" } as BootstrapState;
    const store = useRoleImagesStore();
    store.applyCatalogSnapshot(recipe.projectRef, [recipe]);
    expect(() =>
      store.applyCatalogSnapshot(recipe.projectRef, [
        { ...recipe, organizationRef: "org_foreign" },
      ]),
    ).toThrow();
    expect(store.catalog(recipe.projectRef)).toHaveLength(1);
    platform.bootstrap = { organizationRef: "org_foreign" } as BootstrapState;
    expect(store.catalog(recipe.projectRef)).toEqual([]);
  });
  it("убирает ранее прочитанный исходник после отказа защищённого detail", async () => {
    api.loadRoleImageDetail.mockResolvedValueOnce({ recipe, builds: [] });
    const store = useRoleImagesStore();
    await store.loadDetail(recipe.projectRef, recipe.ref, false);
    expect(store.recipes[recipe.ref]?.environment.dockerfile).toBe(
      "FROM scratch",
    );
    api.loadRoleImageDetail.mockRejectedValueOnce(new Error("Access denied"));
    await store.loadDetail(recipe.projectRef, recipe.ref, false);
    expect(store.recipes[recipe.ref]).toBeUndefined();
    expect(store.builds[recipe.ref]).toBeUndefined();
    expect(store.problem).toBeDefined();
  });
  it("не принимает detail другого проекта после прежнего успешного чтения", async () => {
    api.loadRoleImageDetail
      .mockResolvedValueOnce({ recipe, builds: [] })
      .mockResolvedValueOnce({
        recipe: { ...recipe, projectRef: "other_project" },
        builds: [],
      });
    const store = useRoleImagesStore();
    await store.loadDetail(recipe.projectRef, recipe.ref, false);
    await store.loadDetail(recipe.projectRef, recipe.ref, false);
    expect(store.recipes[recipe.ref]).toBeUndefined();
    expect(store.problem).toBeDefined();
  });
  it("не откатывает ревизию повтором объекта на следующей странице", async () => {
    api.loadRoleImagePage
      .mockResolvedValueOnce({
        items: [recipe],
        total: 43,
        nextPageToken: "next",
      })
      .mockResolvedValueOnce({
        items: [{ ...recipe, version: 2 }],
        total: 43,
        nextPageToken: "",
      });
    const store = useRoleImagesStore();
    await store.loadCatalog(recipe.projectRef);
    await store.loadCatalog(recipe.projectRef, false);
    expect(store.catalog(recipe.projectRef)).toEqual([recipe]);
  });
  it("отклоняет чужой project и повтор курсора до изменения каталога", async () => {
    api.loadRoleImagePage
      .mockResolvedValueOnce({
        items: [recipe],
        total: 43,
        nextPageToken: "next",
      })
      .mockResolvedValueOnce({
        items: [{ ...recipe, projectRef: "other" }],
        total: 43,
        nextPageToken: "",
      })
      .mockResolvedValueOnce({ items: [], total: 43, nextPageToken: "next" });
    const store = useRoleImagesStore();
    await store.loadCatalog(recipe.projectRef);
    await store.loadCatalog(recipe.projectRef, false);
    expect(store.problem).toBeDefined();
    await store.loadCatalog(recipe.projectRef, false);
    expect(store.problem).toBeDefined();
    expect(store.catalog(recipe.projectRef)).toEqual([recipe]);
  });
  it("отменяет запрос при dispose и игнорирует поздний ответ", async () => {
    let resolve!: (page: RoleImageRecipePage) => void;
    api.loadRoleImagePage.mockReturnValue(
      new Promise<RoleImageRecipePage>((ready) => {
        resolve = ready;
      }),
    );
    const store = useRoleImagesStore();
    const pending = store.loadCatalog(recipe.projectRef);
    const signal = api.loadRoleImagePage.mock.calls[0]?.[2] as AbortSignal;
    store.dispose();
    expect(signal.aborted).toBe(true);
    resolve({ items: [recipe], total: 1, nextPageToken: "" });
    await pending;
    expect(store.catalog(recipe.projectRef)).toEqual([]);
  });
  it("передаёт query/state на каждую страницу и сохраняет owner total", async () => {
    api.loadRoleImagePage
      .mockResolvedValueOnce({
        items: [recipe],
        total: 43,
        nextPageToken: "next",
      })
      .mockResolvedValueOnce({ items: [], total: 43 });
    const store = useRoleImagesStore();
    await store.loadCatalog(recipe.projectRef, true, {
      query: "Среда",
      state: "ACTIVE",
    });
    expect(store.projectTotal[recipe.projectRef]).toBe(43);
    await store.loadCatalog(recipe.projectRef, false);
    expect(api.loadRoleImagePage).toHaveBeenLastCalledWith(
      recipe.projectRef,
      "next",
      expect.any(AbortSignal),
      { query: "Среда", state: "ACTIVE" },
      20,
    );
    expect(store.projectTotal[recipe.projectRef]).toBe(43);
  });
  it("обновляет справочник ролей при позднем realtime-снимке агентов", () => {
    const store = useRoleImagesStore();
    const environment: RoleEnvironment = {
      key: "standard",
      nameMessageKey: "roleImages.environment.standard.name",
      descriptionMessageKey: "roleImages.environment.standard.description",
      softwareMessageKeys: [],
      platforms: [],
      recommended: true,
      available: true,
      customInstallationAllowed: false,
      dockerfileTemplate: "FROM scratch",
    };
    const agent: Agent = {
      ref: "agent_synthetic",
      version: 2,
      projectRef: recipe.projectRef,
      roleDefinitionRef: recipe.roleDefinitionRef,
      roleDefinitionName: "Аналитик",
      name: "Аналитик проекта",
      purpose: "Проверять сводки",
      roleDescription: "Аналитик",
      state: "READY",
      enabled: true,
      system: false,
      runtimeRef: "runtime_synthetic",
      runtimeName: "Базовая среда",
      runtimeReady: true,
      capabilities: [],
      integrations: [],
      knowledgeArtifactRefs: [],
      updatedAt: "2026-09-05T00:00:00Z",
      nextActions: [],
    };

    store.applySupportingCatalogSnapshot([], [environment]);
    expect(
      store.roleDefinitionByRef.get(recipe.roleDefinitionRef),
    ).toBeUndefined();
    store.applySupportingCatalogSnapshot([agent], [environment]);

    expect(store.roleDefinitionByRef.get(recipe.roleDefinitionRef)).toEqual({
      ref: recipe.roleDefinitionRef,
      label: "Аналитик",
      agentCount: 1,
    });
    expect(store.environmentByKey.get(environment.key)).toEqual(environment);
  });
  it.each(["dispose", "denied", "different-detail"] as const)(
    "не возвращает исходник из поздней history page после %s",
    async (transition) => {
      const store = useRoleImagesStore();
      store.revisionNextPageToken[recipe.ref] = "next";
      let resolve!: (page: { items: unknown[] }) => void;
      api.loadRoleImageRevisionPage.mockReturnValue(
        new Promise((ready) => {
          resolve = ready;
        }),
      );
      const pending = store.loadMoreRevisions(recipe.projectRef, recipe.ref);
      if (transition === "dispose") store.dispose();
      else {
        if (transition === "denied")
          api.loadRoleImageDetail.mockRejectedValueOnce(
            new Error("Access denied"),
          );
        else
          api.loadRoleImageDetail.mockResolvedValueOnce({
            recipe: { ...recipe, ref: "another_image" },
            builds: [],
          });
        await store.loadDetail(
          recipe.projectRef,
          transition === "denied" ? recipe.ref : "another_image",
          false,
        );
      }
      resolve({
        items: [
          {
            ref: "late_source",
            sourceAvailable: true,
            environment: recipe.environment,
          },
        ],
      });
      await pending;
      expect(store.revisions[recipe.ref]).toBeUndefined();
      expect(store.loadingDetail).toBe(false);
    },
  );
  it("ошибка history очищает уже прочитанный исходник", async () => {
    const store = useRoleImagesStore();
    store.recipes[recipe.ref] = recipe;
    store.revisionNextPageToken[recipe.ref] = "next";
    api.loadRoleImageRevisionPage.mockRejectedValueOnce(
      new Error("Access denied"),
    );
    await store.loadMoreRevisions(recipe.projectRef, recipe.ref);
    expect(store.recipes[recipe.ref]).toBeUndefined();
    expect(store.revisionNextPageToken[recipe.ref]).toBeUndefined();
    expect(store.problem).toBeDefined();
  });
});
