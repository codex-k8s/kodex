import { describe, expect, it, vi } from "vitest";
import {
  verifiedInventoryFixture,
  unavailableInventoryFixture,
} from "@/test-utils/image-inventory-fixture";
import type { RoleImageArtifact } from "@/shared/api/generated/openapi/types.gen";
import {
  createScopedRuntimeImageCatalog,
  type ScopedImageCatalogReader,
} from "./scoped-image-catalog";

const scope = { kind: "ORGANIZATION", organizationRef: "org_alpha" } as const;
const artifact: RoleImageArtifact = {
  projectRef: "",
  scopeKind: "ORGANIZATION",
  organizationRef: "org_alpha",
  ref: "artifact_alpha",
  version: 1,
  recipeRef: "recipe_alpha",
  recipeGeneration: 2,
  buildRef: "build_alpha",
  manifestDigest: `sha256:${"a".repeat(64)}`,
  provenanceSha256: "b".repeat(64),
  promotedReference: `example.invalid/assistant@sha256:${"a".repeat(64)}`,
  admissionVerdict: "ACCEPTED",
  promotionState: "PROMOTED",
  promotionRequested: true,
  declaredTools: [{ name: "git", version: "2.53" }],
  verifiedToolInventory: verifiedInventoryFixture(),
};
const recipe: Awaited<ReturnType<ScopedImageCatalogReader["read"]>>["recipe"] =
  {
    ref: artifact.recipeRef,
    version: 4,
    projectRef: "",
    scopeKind: "ORGANIZATION",
    organizationRef: scope.organizationRef,
    roleDefinitionRef: "role_alpha",
    name: "Среда помощника",
    state: "ACTIVE",
    generation: 3,
    sourceAvailable: false,
    environment: { environmentKey: "standard" },
    promotedImageReady: true,
    activeImageArtifactRef: artifact.ref,
    promotedImageReference: artifact.promotedReference,
    nextActions: ["OPEN"],
    createdAt: "2026-10-04T00:00:00Z",
    updatedAt: "2026-10-04T00:00:00Z",
  };
function reader() {
  return {
    list: vi
      .fn<ScopedImageCatalogReader["list"]>()
      .mockResolvedValue({ items: [recipe], total: 1, nextPageToken: "" }),
    read: vi
      .fn<ScopedImageCatalogReader["read"]>()
      .mockResolvedValue({ recipe, builds: [], activeArtifact: artifact }),
  };
}
const signal = () => new AbortController().signal;

describe("Каталог допущенных образов в точной области", () => {
  it.each([recipe.ref, artifact.promotedReference])(
    "сохраняет поиск по полному ref рецепта и image reference",
    async (query) => {
      if (!query) throw new Error("Missing synthetic catalog query");
      const api = reader();
      const page = await createScopedRuntimeImageCatalog(
        scope.organizationRef,
        api,
      ).loadPage(scope, query, undefined, signal());
      expect(page.items.map((item) => item.ref)).toEqual([artifact.ref]);
      expect(api.list).toHaveBeenCalledTimes(1);
      expect(api.read).toHaveBeenCalledTimes(1);
    },
  );
  it("исторический UNAVAILABLE не скрывает соседний точный образ", async () => {
    const api = reader();
    const historical = {
      ...recipe,
      ref: "recipe_historical",
      activeImageArtifactRef: "artifact_historical",
    };
    api.list.mockResolvedValue({ items: [historical, recipe], total: 2 });
    api.read.mockImplementation((_, ref) =>
      Promise.resolve(
        ref === historical.ref
          ? {
              recipe: historical,
              builds: [],
              activeArtifact: {
                ...artifact,
                ref: historical.activeImageArtifactRef,
                recipeRef: historical.ref,
                verifiedToolInventory: unavailableInventoryFixture(),
              },
            }
          : { recipe, builds: [], activeArtifact: artifact },
      ),
    );
    const catalog = createScopedRuntimeImageCatalog(scope.organizationRef, api);
    const page = await catalog.loadPage(scope, "", undefined, signal());
    expect(page.items.map((item) => item.ref)).toEqual([artifact.ref]);
    await expect(
      catalog.loadArtifact(
        scope,
        historical.ref,
        historical.activeImageArtifactRef,
        signal(),
      ),
    ).rejects.toMatchObject({ code: "IMAGE_ARTIFACT_NOT_CURRENT" });
  });
  it("проходит историческую пустую страницу по курсору без ослабления inventory", async () => {
    const api = reader();
    const historical = {
      ...recipe,
      ref: "recipe_historical",
      activeImageArtifactRef: "artifact_historical",
    };
    api.list
      .mockResolvedValueOnce({
        items: [historical],
        total: 2,
        nextPageToken: "next",
      })
      .mockResolvedValueOnce({ items: [recipe], total: 2 });
    api.read.mockImplementation((_, ref) =>
      Promise.resolve(
        ref === historical.ref
          ? {
              recipe: historical,
              builds: [],
              activeArtifact: {
                ...artifact,
                ref: historical.activeImageArtifactRef,
                recipeRef: historical.ref,
                verifiedToolInventory: unavailableInventoryFixture(),
              },
            }
          : { recipe, builds: [], activeArtifact: artifact },
      ),
    );
    const page = await createScopedRuntimeImageCatalog(
      scope.organizationRef,
      api,
    ).loadPage(scope, "", undefined, signal());
    expect(page.items.map((item) => item.ref)).toEqual([artifact.ref]);
    expect(api.list).toHaveBeenNthCalledWith(
      2,
      scope,
      "next",
      expect.any(AbortSignal),
      30,
    );
  });
  it.each(["foreign", "identity", "verified", "transport"])(
    "не скрывает ошибку %s соседнего кандидата",
    async (failure) => {
      const api = reader();
      const historical = {
        ...recipe,
        ref: "recipe_historical",
        activeImageArtifactRef: "artifact_historical",
      };
      api.list.mockResolvedValue({ items: [historical, recipe], total: 2 });
      api.read.mockImplementation((_, ref) => {
        if (ref !== historical.ref)
          return Promise.resolve({
            recipe,
            builds: [],
            activeArtifact: artifact,
          });
        if (failure === "transport")
          throw new Error("Synthetic transport failure");
        const invalid = {
          ...artifact,
          ref: historical.activeImageArtifactRef,
          recipeRef: historical.ref,
          verifiedToolInventory:
            failure === "verified"
              ? { ...verifiedInventoryFixture(), sha256: "" }
              : unavailableInventoryFixture(),
          ...(failure === "foreign" ? { organizationRef: "org_foreign" } : {}),
          ...(failure === "identity" ? { ref: "artifact_other" } : {}),
        };
        return Promise.resolve({
          recipe: historical,
          builds: [],
          activeArtifact: invalid,
        });
      });
      await expect(
        createScopedRuntimeImageCatalog(scope.organizationRef, api).loadPage(
          scope,
          "",
          undefined,
          signal(),
        ),
      ).rejects.toThrow();
    },
  );
  it("выбирает поколение активного артефакта, а не новой ещё не опубликованной сборки", async () => {
    const catalog = createScopedRuntimeImageCatalog(
      scope.organizationRef,
      reader(),
    );
    const page = await catalog.loadPage(scope, "", undefined, signal());
    expect(page.items[0]).toMatchObject({
      ref: artifact.ref,
      recipeRef: recipe.ref,
      generation: 2,
    });
  });
  it("не отправляет запрос для чужой организации", async () => {
    const api = reader();
    const catalog = createScopedRuntimeImageCatalog(scope.organizationRef, api);
    await expect(
      catalog.loadPage(
        { ...scope, organizationRef: "org_beta" },
        "",
        undefined,
        signal(),
      ),
    ).rejects.toThrow("scope mismatch");
    expect(api.list).not.toHaveBeenCalled();
  });
  it.each([
    { ...recipe, organizationRef: "org_beta" },
    { ...recipe, scopeKind: "UNSPECIFIED" as const },
    { ...recipe, scopeKind: "PROJECT" as const, projectRef: "prj_alpha" },
  ])("не выдаёт каталог другого owner/scope", async (invalid) => {
    const api = reader();
    api.list.mockResolvedValue({
      items: [invalid],
      total: 1,
      nextPageToken: "",
    });
    await expect(
      createScopedRuntimeImageCatalog(scope.organizationRef, api).loadPage(
        scope,
        "",
        undefined,
        signal(),
      ),
    ).rejects.toThrow("scope mismatch");
    expect(api.read).not.toHaveBeenCalled();
  });
  it.each([
    { ref: "artifact_beta" },
    { recipeRef: "recipe_beta" },
    { organizationRef: "org_beta" },
    { scopeKind: "PROJECT" as const },
    { promotionState: "AUTHORIZED" as const },
    { admissionVerdict: "REJECTED" as const },
    { promotedReference: "example.invalid/assistant:latest" },
  ])("не подменяет точный admitted artifact", async (change) => {
    const api = reader();
    api.read.mockResolvedValue({
      recipe,
      builds: [],
      activeArtifact: { ...artifact, ...change },
    });
    await expect(
      createScopedRuntimeImageCatalog(scope.organizationRef, api).loadArtifact(
        scope,
        recipe.ref,
        artifact.ref,
        signal(),
      ),
    ).rejects.toThrow();
  });
  it("останавливает повторяющийся курсор даже если страница пустая", async () => {
    const api = reader();
    api.list.mockResolvedValue({
      items: [],
      total: 0,
      nextPageToken: "cursor_alpha",
    });
    await expect(
      createScopedRuntimeImageCatalog(scope.organizationRef, api).loadPage(
        scope,
        "",
        "cursor_alpha",
        signal(),
      ),
    ).rejects.toThrow("repeated cursor");
  });
  it("не возвращает поздний ответ после отмены чтения", async () => {
    const controller = new AbortController();
    const api = reader();
    api.list.mockImplementation(() => {
      controller.abort();
      return Promise.resolve({ items: [recipe], total: 1, nextPageToken: "" });
    });
    await expect(
      createScopedRuntimeImageCatalog(scope.organizationRef, api).loadPage(
        scope,
        "",
        undefined,
        controller.signal,
      ),
    ).rejects.toThrow();
    expect(api.read).not.toHaveBeenCalled();
  });
});
