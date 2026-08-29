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
  type RoleDefinitionOption,
  updateRoleImage,
} from "@/features/role-images/api";
import type {
  RoleEnvironment,
  RoleImageArtifact,
  RoleImageBuild,
  RoleImageRecipe,
  RoleImageRecipeCommand,
  RoleImageRecipeCreateInput,
  RoleImageRecipeUpdateInput,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export const useRoleImagesStore = defineStore("role-images", () => {
  const recipes = reactive<Record<string, RoleImageRecipe>>({});
  const builds = reactive<Record<string, RoleImageBuild[]>>({});
  const artifacts = reactive<Record<string, RoleImageArtifact | undefined>>({});
  const dependencies = reactive<Record<string, RuntimeEnvironmentSet[]>>({});
  const projectRecipeRefs = reactive<Record<string, string[]>>({});
  const projectNextPageToken = reactive<Record<string, string | undefined>>({});
  const roleDefinitions = ref<RoleDefinitionOption[]>([]);
  const environments = ref<RoleEnvironment[]>([]);
  const loadingCatalog = ref(false);
  const loadingMore = ref(false);
  const loadingDetail = ref(false);
  const mutating = ref(false);
  const problem = ref<AppProblem>();
  let catalogGeneration = 0;
  let detailGeneration = 0;

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

  async function loadCatalog(projectRef: string, reset = true): Promise<void> {
    if (!reset && !projectNextPageToken[projectRef]) return;
    const current = ++catalogGeneration;
    if (reset) loadingCatalog.value = true;
    else loadingMore.value = true;
    problem.value = undefined;
    try {
      const page = await loadRoleImagePage(
        projectRef,
        reset ? undefined : projectNextPageToken[projectRef],
      );
      if (current !== catalogGeneration) return;
      const refs = reset ? [] : [...(projectRecipeRefs[projectRef] ?? [])];
      const seen = new Set(refs);
      for (const recipe of page.items) {
        recipes[recipe.ref] = recipe;
        if (!seen.has(recipe.ref)) {
          seen.add(recipe.ref);
          refs.push(recipe.ref);
        }
      }
      projectRecipeRefs[projectRef] = refs;
      projectNextPageToken[projectRef] = page.nextPageToken;
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
  ): Promise<void> {
    const current = ++detailGeneration;
    loadingDetail.value = true;
    problem.value = undefined;
    try {
      const detail = await loadRoleImageDetail(projectRef, recipeRef);
      if (current !== detailGeneration) return;
      const dependencyItems = detail.activeArtifact
        ? await loadRoleImageDependencies(projectRef, detail.activeArtifact.ref)
        : [];
      if (current !== detailGeneration) return;
      recipes[detail.recipe.ref] = detail.recipe;
      builds[detail.recipe.ref] = detail.builds;
      artifacts[detail.recipe.ref] = detail.activeArtifact;
      dependencies[detail.recipe.ref] = dependencyItems;
    } catch (error) {
      if (current === detailGeneration) problem.value = asProblem(error);
    } finally {
      if (current === detailGeneration) loadingDetail.value = false;
    }
  }

  async function loadSupportingCatalogs(projectRef: string): Promise<void> {
    problem.value = undefined;
    try {
      const [definitions, environmentCatalog] = await Promise.all([
        loadRoleDefinitionOptions(projectRef),
        loadRoleEnvironmentCatalog(),
      ]);
      roleDefinitions.value = definitions;
      environments.value = environmentCatalog;
    } catch (error) {
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

  function dispose(): void {
    catalogGeneration += 1;
    detailGeneration += 1;
  }

  return {
    recipes,
    builds,
    artifacts,
    dependencies,
    projectNextPageToken,
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
    loadSupportingCatalogs,
    create,
    update,
    command,
    dispose,
  };
});
