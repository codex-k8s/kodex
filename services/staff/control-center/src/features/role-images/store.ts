import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";

import {
  commandRoleImage,
  createRoleImage,
  loadRoleDefinitionOptions,
  loadRoleEnvironmentCatalog,
  loadRoleImageDependencies,
  loadRoleImageDetail,
  loadRoleImagePage,
  loadRoleImageRevisionPage,
  promoteRoleImageArtifact,
  type RoleDefinitionOption,
  updateRoleImage,
} from "@/features/role-images/api";
import type {
  RoleEnvironment,
  RoleImageArtifact,
  RoleImageBuild,
  RoleImageRecipe,
  RoleImageRecipeRevision,
  RoleImageRecipeCommand,
  RoleImageRecipeCreateInput,
  RoleImageRecipeUpdateInput,
  RoleImagePromotionReceipt,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export const useRoleImagesStore = defineStore("role-images", () => {
  const recipes = reactive<Record<string, RoleImageRecipe>>({});
  const builds = reactive<Record<string, RoleImageBuild[]>>({});
  const artifacts = reactive<Record<string, RoleImageArtifact | undefined>>({});
  const revisions = reactive<Record<string, RoleImageRecipeRevision[]>>({});
  const revisionNextPageToken = reactive<Record<string, string | undefined>>(
    {},
  );
  const promotionReceipts = reactive<
    Record<string, RoleImagePromotionReceipt | undefined>
  >({});
  const dependencies = reactive<Record<string, RuntimeEnvironmentSet[]>>({});
  const projectRecipeRefs = reactive<Record<string, string[]>>({});
  const projectNextPageToken = reactive<Record<string, string | undefined>>({});
  const projectTotal = reactive<Record<string, number | undefined>>({});
  const catalogFilters = new Map<
    string,
    { query?: string; state?: "ACTIVE" | "ARCHIVED" }
  >();
  const roleDefinitions = ref<RoleDefinitionOption[]>([]);
  const environments = ref<RoleEnvironment[]>([]);
  const loadingCatalog = ref(false);
  const loadingMore = ref(false);
  const loadingDetail = ref(false);
  const mutating = ref(false);
  const problem = ref<AppProblem>();
  let catalogGeneration = 0;
  let catalogController: AbortController | undefined;
  let detailGeneration = 0;
  let supportingGeneration = 0;
  let supportingController: AbortController | undefined;

  const environmentByKey = computed(
    () => new Map(environments.value.map((value) => [value.key, value])),
  );
  const roleDefinitionByRef = computed(
    () => new Map(roleDefinitions.value.map((value) => [value.ref, value])),
  );

  function catalog(projectRef: string): RoleImageRecipe[] {
    return (projectRecipeRefs[projectRef] ?? [])
      .map((ref) => recipes[ref])
      .filter((value): value is RoleImageRecipe => Boolean(value));
  }

  async function loadCatalog(
    projectRef: string,
    reset = true,
    filter?: { query?: string; state?: "ACTIVE" | "ARCHIVED" },
  ): Promise<void> {
    if (
      !reset &&
      (!projectNextPageToken[projectRef] ||
        loadingCatalog.value ||
        loadingMore.value)
    )
      return;
    catalogController?.abort();
    const controller = new AbortController();
    catalogController = controller;
    const cursor = reset ? undefined : projectNextPageToken[projectRef];
    const current = ++catalogGeneration;
    if (filter) catalogFilters.set(projectRef, filter);
    const activeFilter = catalogFilters.get(projectRef) ?? {};
    if (reset) {
      projectRecipeRefs[projectRef] = [];
      projectNextPageToken[projectRef] = undefined;
      projectTotal[projectRef] = undefined;
    }
    if (reset) loadingCatalog.value = true;
    else loadingMore.value = true;
    problem.value = undefined;
    try {
      const page = await loadRoleImagePage(
        projectRef,
        cursor,
        controller.signal,
        activeFilter,
      );
      if (current !== catalogGeneration) return;
      if (
        !Array.isArray(page.items) ||
        !Number.isSafeInteger(page.total) ||
        page.total < page.items.length ||
        page.items.some(
          (recipe) =>
            recipe.projectRef !== projectRef ||
            (activeFilter.state && recipe.state !== activeFilter.state),
        ) ||
        (page.nextPageToken && page.nextPageToken === cursor)
      )
        throw new Error("Invalid role image catalog scope or cursor");
      const refs = reset ? [] : [...(projectRecipeRefs[projectRef] ?? [])];
      const seen = new Set(refs);
      for (const recipe of page.items) {
        if (seen.has(recipe.ref))
          throw new Error("Repeated role image catalog item");
        const previous = recipes[recipe.ref];
        if (!previous || previous.version <= recipe.version)
          recipes[recipe.ref] = recipe;
        if (!seen.has(recipe.ref)) {
          seen.add(recipe.ref);
          refs.push(recipe.ref);
        }
      }
      projectRecipeRefs[projectRef] = refs;
      projectNextPageToken[projectRef] = page.nextPageToken;
      projectTotal[projectRef] = page.total;
    } catch (error) {
      if (current === catalogGeneration) problem.value = asProblem(error);
    } finally {
      if (current === catalogGeneration) {
        loadingCatalog.value = false;
        loadingMore.value = false;
      }
    }
  }

  async function loadDetail(
    projectRef: string,
    recipeRef: string,
    showLoading = true,
  ): Promise<void> {
    const current = ++detailGeneration;
    if (showLoading) loadingDetail.value = true;
    problem.value = undefined;
    try {
      const detail = await loadRoleImageDetail(projectRef, recipeRef);
      if (current !== detailGeneration) return;
      if (!showLoading) {
        recipes[detail.recipe.ref] = detail.recipe;
        builds[detail.recipe.ref] = detail.builds;
        artifacts[detail.recipe.ref] =
          detail.promotionCandidate ?? detail.activeArtifact;
        return;
      }
      const [dependencyItems, revisionPage] = await Promise.all([
        detail.activeArtifact
          ? loadRoleImageDependencies(projectRef, detail.activeArtifact.ref)
          : Promise.resolve([]),
        loadRoleImageRevisionPage(projectRef, recipeRef),
      ]);
      if (current !== detailGeneration) return;
      recipes[detail.recipe.ref] = detail.recipe;
      builds[detail.recipe.ref] = detail.builds;
      artifacts[detail.recipe.ref] =
        detail.promotionCandidate ?? detail.activeArtifact;
      dependencies[detail.recipe.ref] = dependencyItems;
      revisions[detail.recipe.ref] = revisionPage.items;
      revisionNextPageToken[detail.recipe.ref] = revisionPage.nextPageToken;
    } catch (error) {
      if (current === detailGeneration) problem.value = asProblem(error);
    } finally {
      if (current === detailGeneration && showLoading)
        loadingDetail.value = false;
    }
  }

  async function loadMoreRevisions(
    projectRef: string,
    recipeRef: string,
  ): Promise<void> {
    const pageToken = revisionNextPageToken[recipeRef];
    if (!pageToken || loadingDetail.value) return;
    loadingDetail.value = true;
    problem.value = undefined;
    try {
      const page = await loadRoleImageRevisionPage(
        projectRef,
        recipeRef,
        pageToken,
      );
      const merged = new Map(
        (revisions[recipeRef] ?? []).map((item) => [item.ref, item]),
      );
      for (const item of page.items) merged.set(item.ref, item);
      revisions[recipeRef] = [...merged.values()];
      revisionNextPageToken[recipeRef] = page.nextPageToken;
    } catch (error) {
      problem.value = asProblem(error);
    } finally {
      loadingDetail.value = false;
    }
  }

  async function loadSupportingCatalogs(projectRef: string): Promise<void> {
    const current = ++supportingGeneration;
    supportingController?.abort();
    const controller = new AbortController();
    supportingController = controller;
    roleDefinitions.value = [];
    environments.value = [];
    problem.value = undefined;
    try {
      const [definitions, environmentCatalog] = await Promise.all([
        loadRoleDefinitionOptions(projectRef, controller.signal),
        loadRoleEnvironmentCatalog(controller.signal),
      ]);
      if (current !== supportingGeneration) return;
      roleDefinitions.value = definitions;
      environments.value = environmentCatalog;
    } catch (error) {
      if (current === supportingGeneration && !controller.signal.aborted)
        problem.value = asProblem(error);
    }
  }

  async function command(
    projectRef: string,
    recipe: RoleImageRecipe,
    action: RoleImageRecipeCommand["action"],
  ): Promise<void> {
    mutating.value = true;
    problem.value = undefined;
    try {
      const receipt = await commandRoleImage(projectRef, recipe, action);
      recipes[receipt.recipe.ref] = receipt.recipe;
      if (receipt.imageBuild) {
        const current = builds[receipt.recipe.ref] ?? [];
        builds[receipt.recipe.ref] = [
          receipt.imageBuild,
          ...current.filter((item) => item.ref !== receipt.imageBuild?.ref),
        ];
      }
      await loadDetail(projectRef, receipt.recipe.ref);
    } catch (error) {
      problem.value = asProblem(error);
      throw error;
    } finally {
      mutating.value = false;
    }
  }

  async function create(
    projectRef: string,
    input: RoleImageRecipeCreateInput,
  ): Promise<RoleImageRecipe> {
    mutating.value = true;
    problem.value = undefined;
    try {
      const recipe = await createRoleImage(projectRef, input);
      recipes[recipe.ref] = recipe;
      return recipe;
    } catch (error) {
      problem.value = asProblem(error);
      throw error;
    } finally {
      mutating.value = false;
    }
  }

  async function update(
    projectRef: string,
    recipe: RoleImageRecipe,
    input: RoleImageRecipeUpdateInput,
  ): Promise<RoleImageRecipe> {
    mutating.value = true;
    problem.value = undefined;
    try {
      const saved = await updateRoleImage(projectRef, recipe, input);
      recipes[saved.ref] = saved;
      await loadDetail(projectRef, saved.ref);
      return saved;
    } catch (error) {
      problem.value = asProblem(error);
      throw error;
    } finally {
      mutating.value = false;
    }
  }

  async function promote(
    projectRef: string,
    recipe: RoleImageRecipe,
    artifact: RoleImageArtifact,
  ): Promise<RoleImagePromotionReceipt> {
    mutating.value = true;
    problem.value = undefined;
    try {
      const receipt = await promoteRoleImageArtifact(
        projectRef,
        recipe,
        artifact.ref,
        artifact.provenanceSha256,
      );
      promotionReceipts[recipe.ref] = receipt;
      await loadDetail(projectRef, recipe.ref);
      return receipt;
    } catch (error) {
      problem.value = asProblem(error);
      throw error;
    } finally {
      mutating.value = false;
    }
  }

  function dispose(): void {
    catalogController?.abort();
    supportingController?.abort();
    supportingGeneration += 1;
    catalogGeneration += 1;
    detailGeneration += 1;
  }

  return {
    recipes,
    builds,
    artifacts,
    revisions,
    revisionNextPageToken,
    promotionReceipts,
    dependencies,
    projectNextPageToken,
    projectTotal,
    roleDefinitions,
    environments,
    environmentByKey,
    roleDefinitionByRef,
    loadingCatalog,
    loadingMore,
    loadingDetail,
    mutating,
    problem,
    catalog,
    loadCatalog,
    loadDetail,
    loadMoreRevisions,
    loadSupportingCatalogs,
    create,
    update,
    promote,
    command,
    dispose,
  };
});
