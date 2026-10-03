import { describe, expect, it, vi } from "vitest";
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
  tools: [{ name: "git", version: "2.53" }],
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
