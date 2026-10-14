import type {
  RoleImageRecipe,
  RoleImageRecipeDetail,
  RoleImageRecipePage,
} from "@/shared/api/generated/openapi/types.gen";
import {
  assertPromotedRuntimeImage,
  assertPromotedRuntimeImageIdentity,
  type RuntimeImageCatalog,
  type RuntimeImageOption,
} from "./image-tools-selection";
import {
  assertRuntimeResourceIdentity,
  runtimeResourceScopeKey,
  type RuntimeResourceScope,
  type RuntimeScopedResourceIdentity,
} from "./resource-scope";
import { AppProblem } from "@/shared/api/problem";

class HistoricalImageInventoryUnavailable extends AppProblem {
  constructor() {
    super({
      status: 409,
      code: "IMAGE_ARTIFACT_NOT_CURRENT",
      retryable: false,
      kind: "conflict",
    });
  }
}

type ScopedRecipe = Omit<RoleImageRecipe, "scopeKind"> &
  RuntimeScopedResourceIdentity;
type ScopedDetail = Omit<RoleImageRecipeDetail, "recipe"> & {
  recipe: ScopedRecipe;
};

export interface ScopedImageCatalogReader {
  list(
    scope: RuntimeResourceScope,
    cursor: string | undefined,
    signal: AbortSignal,
    pageSize: number,
  ): Promise<Omit<RoleImageRecipePage, "items"> & { items: ScopedRecipe[] }>;
  read(
    scope: RuntimeResourceScope,
    recipeRef: string,
    signal: AbortSignal,
  ): Promise<ScopedDetail>;
}

export function createScopedRuntimeImageCatalog(
  organizationRef: string,
  reader: ScopedImageCatalogReader,
): RuntimeImageCatalog {
  if (!/^[A-Za-z0-9_-]{8,128}$/.test(organizationRef))
    throw new Error("Runtime image catalog organization is invalid");

  function assertScope(scope: RuntimeResourceScope): void {
    runtimeResourceScopeKey(scope);
    if (
      scope.kind === "ORGANIZATION" &&
      scope.organizationRef !== organizationRef
    )
      throw new Error("Runtime image catalog organization scope mismatch");
  }

  async function readArtifact(
    scope: RuntimeResourceScope,
    recipeRef: string,
    artifactRef: string,
    signal: AbortSignal,
  ) {
    assertScope(scope);
    const detail = await reader.read(scope, recipeRef, signal);
    signal.throwIfAborted();
    assertRuntimeResourceIdentity(scope, detail.recipe, organizationRef);
    if (detail.recipe.ref !== recipeRef)
      throw new Error("Runtime image catalog artifact identity is invalid");
    if (!detail.activeArtifact)
      throw new AppProblem({
        status: 409,
        code: "IMAGE_ARTIFACT_NOT_CURRENT",
        retryable: false,
        kind: "conflict",
      });
    assertRuntimeResourceIdentity(
      scope,
      detail.activeArtifact,
      organizationRef,
    );
    const expected = {
      artifactRef,
      recipeRef,
      recipeGeneration: detail.activeArtifact.recipeGeneration,
    };
    assertPromotedRuntimeImageIdentity(detail.activeArtifact, expected);
    if (detail.recipe.state !== "ACTIVE")
      throw new Error("Runtime image catalog artifact is not active");
    if (
      !detail.recipe.promotedImageReady ||
      detail.recipe.activeImageArtifactRef !== artifactRef
    )
      throw new AppProblem({
        status: 409,
        code: "IMAGE_ARTIFACT_NOT_CURRENT",
        retryable: false,
        kind: "conflict",
      });
    const inventory = detail.activeArtifact.verifiedToolInventory;
    if (
      inventory.status === "UNAVAILABLE" &&
      inventory.sha256 === "" &&
      inventory.imageDigest === "" &&
      inventory.provenanceSha256 === "" &&
      Array.isArray(inventory.platforms) &&
      inventory.platforms.length === 0
    )
      throw new HistoricalImageInventoryUnavailable();
    assertPromotedRuntimeImage(detail.activeArtifact, expected);
    return { artifact: detail.activeArtifact, recipeName: detail.recipe.name };
  }

  return {
    loadArtifact: readArtifact,
    async loadPage(scope, query, cursor, signal, pageSize = 30) {
      assertScope(scope);
      const needle = query.trim().toLocaleLowerCase();
      const visited = new Set<string>(cursor ? [cursor] : []);
      let next = cursor;
      for (let pageNumber = 0; pageNumber < 100; pageNumber += 1) {
        const page = await reader.list(scope, next, signal, pageSize);
        signal.throwIfAborted();
        if (
          !Array.isArray(page.items) ||
          !Number.isSafeInteger(page.total) ||
          page.total < page.items.length ||
          new Set(page.items.map((item) => item.ref)).size !== page.items.length
        )
          throw new Error("Runtime image catalog page is invalid");
        for (const recipe of page.items)
          assertRuntimeResourceIdentity(scope, recipe, organizationRef);
        const candidates = page.items.filter(
          (recipe) =>
            recipe.state === "ACTIVE" &&
            recipe.promotedImageReady &&
            recipe.activeImageArtifactRef &&
            (!needle ||
              recipe.name.toLocaleLowerCase().includes(needle) ||
              recipe.ref.toLocaleLowerCase().includes(needle) ||
              recipe.promotedImageReference
                ?.toLocaleLowerCase()
                .includes(needle)),
        );
        const options = await Promise.all(
          candidates.map(async (recipe) => {
            const ref = recipe.activeImageArtifactRef;
            if (!ref)
              throw new Error(
                "Runtime image artifact reference is unavailable",
              );
            let result;
            try {
              result = await readArtifact(scope, recipe.ref, ref, signal);
            } catch (error) {
              // Исторический UNAVAILABLE не предлагается и не закрывает соседний
              // точный образ. Другие ошибки, включая owner/VERIFIED, не скрываются.
              if (error instanceof HistoricalImageInventoryUnavailable)
                return undefined;
              throw error;
            }
            return {
              ref,
              title: result.recipeName,
              description: result.artifact.promotedReference,
              recipeRef: recipe.ref,
              generation: result.artifact.recipeGeneration,
            };
          }),
        );
        const items: RuntimeImageOption[] = options.filter(
          (option) => option !== undefined,
        );
        if (page.nextPageToken && visited.has(page.nextPageToken))
          throw new Error("Runtime image catalog returned a repeated cursor");
        if (items.length || !page.nextPageToken)
          return { items, nextPageToken: page.nextPageToken };
        next = page.nextPageToken;
        visited.add(next);
      }
      throw new Error("Runtime image catalog page budget is exhausted");
    },
  };
}
