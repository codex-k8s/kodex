import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  RoleImageRecipe,
  RoleImageRecipePage,
} from "@/shared/api/generated/openapi/types.gen";

const api = vi.hoisted(() => ({ loadRoleImagePage: vi.fn() }));
vi.mock("./api", () => api);
import { useRoleImagesStore } from "./store";

const recipe: RoleImageRecipe = {
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
  it("не откатывает ревизию повтором объекта на следующей странице", async () => {
    api.loadRoleImagePage
      .mockResolvedValueOnce({ items: [recipe], nextPageToken: "next" })
      .mockResolvedValueOnce({
        items: [{ ...recipe, version: 2 }],
        nextPageToken: "",
      });
    const store = useRoleImagesStore();
    await store.loadCatalog(recipe.projectRef);
    await store.loadCatalog(recipe.projectRef, false);
    expect(store.catalog(recipe.projectRef)).toEqual([recipe]);
  });
  it("отклоняет чужой project и повтор курсора до изменения каталога", async () => {
    api.loadRoleImagePage
      .mockResolvedValueOnce({ items: [recipe], nextPageToken: "next" })
      .mockResolvedValueOnce({
        items: [{ ...recipe, projectRef: "other" }],
        nextPageToken: "",
      })
      .mockResolvedValueOnce({ items: [], nextPageToken: "next" });
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
    resolve({ items: [recipe], nextPageToken: "" });
    await pending;
    expect(store.catalog(recipe.projectRef)).toEqual([]);
  });
});
