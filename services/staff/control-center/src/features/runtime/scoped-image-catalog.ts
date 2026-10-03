import type {
  RoleImageRecipe,
  RoleImageRecipeDetail,
  RoleImageRecipePage,
} from "@/shared/api/generated/openapi/types.gen";
import {
  assertPromotedRuntimeImage,
  type RuntimeImageCatalog,
  type RuntimeImageOption,
} from "./image-tools-selection";
import {
  assertRuntimeResourceIdentity,
  runtimeResourceScopeKey,
  type RuntimeResourceScope,
  type RuntimeScopedResourceIdentity,
} from "./resource-scope";

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
    if (detail.recipe.ref !== recipeRef || !detail.activeArtifact)
      throw new Error("Runtime image catalog artifact identity is invalid");
    assertRuntimeResourceIdentity(
      scope,
      detail.activeArtifact,
      organizationRef,
    );
    assertPromotedRuntimeImage(detail.activeArtifact, {
      artifactRef,
      recipeRef,
      recipeGeneration: detail.activeArtifact.recipeGeneration,
    });
    if (
      detail.recipe.state !== "ACTIVE" ||
      !detail.recipe.promotedImageReady ||
      detail.recipe.activeImageArtifactRef !== artifactRef
    )
      throw new Error("Runtime image catalog artifact is not active");
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
              recipe.promotedImageReference
                ?.toLocaleLowerCase()
                .includes(needle)),
        );
        const items: RuntimeImageOption[] = await Promise.all(
          candidates.map(async (recipe) => {
            const ref = recipe.activeImageArtifactRef;
            if (!ref)
              throw new Error(
                "Runtime image artifact reference is unavailable",
              );
            const result = await readArtifact(scope, recipe.ref, ref, signal);
            return {
              ref,
              title: result.recipeName,
              description: result.artifact.promotedReference,
              recipeRef: recipe.ref,
              generation: result.artifact.recipeGeneration,
            };
          }),
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
