import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RoleImageRecipe,
  RoleImageRecipePage,
} from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({
  loadRoleImagePage: vi.fn(),
  loadRoleImageDetail: vi.fn(),
}));
vi.mock("./api", () => api);
import { useRoleImagesStore } from "./store";

const recipe: RoleImageRecipe = {
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
    vi.resetAllMocks();
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
    );
    expect(store.projectTotal[recipe.projectRef]).toBe(43);
  });
});
