import { defineStore } from "pinia";
import { computed, reactive, ref, watch } from "vue";
import { usePlatformStore } from "@/features/platform/store";
import { requireRuntimeOrganizationRef } from "@/features/runtime/resource-scope";
import { runtimeResourceOwnerBoundary } from "@/features/runtime/active-resource-owner";

import {
  commandRoleImage,
  createRoleImage,
  loadRoleDefinitionOptions,
  loadRoleEnvironmentCatalog,
  loadRoleImageDependencies,
  loadRoleImageCreateAccess,
  loadRoleImageDetail,
  loadRoleImagePage,
  loadRoleImageRevisionPage,
  promoteRoleImageArtifact,
  type RoleDefinitionOption,
  updateRoleImage,
} from "@/features/role-images/api";
import type {
  Agent,
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
import {
  assertRoleImageResourceIdentity,
  roleImageScopeKey,
  roleImageRuntimeScope,
  type RoleImageResourceScope,
} from "./resource-scope";

export const useRoleImagesStore = defineStore("role-images", () => {
  const platform = usePlatformStore();
  const assertOwned: typeof assertRoleImageResourceIdentity = (
    scope,
    resource,
    organizationRef,
  ) => {
    const currentOrganizationRef = requireRuntimeOrganizationRef(
      platform.bootstrap?.organizationRef,
    );
    if (organizationRef && organizationRef !== currentOrganizationRef)
      throw new Error("Role image request organization anchor changed");
    assertRoleImageResourceIdentity(scope, resource, currentOrganizationRef);
  };
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
  const createAllowed = reactive<Record<string, boolean>>({});
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

  function catalog(scope: RoleImageResourceScope): RoleImageRecipe[] {
    const projectRef = roleImageScopeKey(scope);
    return (projectRecipeRefs[projectRef] ?? [])
      .map((ref) => recipes[ref])
      .filter((value): value is RoleImageRecipe => {
        if (!value) return false;
        try {
          assertOwned(scope, value);
          return true;
        } catch {
          return false;
        }
      });
  }

  function applyCatalogSnapshot(
    scope: RoleImageResourceScope,
    values: RoleImageRecipe[],
    nextPageToken?: string,
    total?: number,
  ): void {
    runtimeResourceOwnerBoundary(scope);
    const projectRef = roleImageScopeKey(scope);
    for (const value of values) assertOwned(scope, value);
    if (new Set(values.map((recipe) => recipe.ref)).size !== values.length)
      throw new Error("Invalid role image realtime catalog scope");
    const refs = new Set(values.map((recipe) => recipe.ref));
    for (const ref of projectRecipeRefs[projectRef] ?? [])
      if (!refs.has(ref)) Reflect.deleteProperty(recipes, ref);
    for (const recipe of values) {
      const previous = recipes[recipe.ref];
      if (!previous || previous.version <= recipe.version)
        recipes[recipe.ref] = recipe;
    }
    projectRecipeRefs[projectRef] = values.map((recipe) => recipe.ref);
    projectNextPageToken[projectRef] = nextPageToken;
    projectTotal[projectRef] = total;
    problem.value = undefined;
    loadingCatalog.value = false;
  }

  async function loadCatalog(
    scope: RoleImageResourceScope,
    reset = true,
    filter?: { query?: string; state?: "ACTIVE" | "ARCHIVED" },
    pageSize = 20,
  ): Promise<void> {
    const projectRef = roleImageScopeKey(scope);
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
        scope,
        cursor,
        controller.signal,
        activeFilter,
        pageSize,
      );
      if (current !== catalogGeneration) return;
      if (
        !Array.isArray(page.items) ||
        !Number.isSafeInteger(page.total) ||
        page.total < page.items.length ||
        page.items.some(
          (recipe) => activeFilter.state && recipe.state !== activeFilter.state,
        ) ||
        (page.nextPageToken && page.nextPageToken === cursor)
      )
        throw new Error("Invalid role image catalog scope or cursor");
      for (const recipe of page.items) assertOwned(scope, recipe);
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

  function clearDetail(recipeRef: string): void {
    for (const cache of [
      recipes,
      builds,
      artifacts,
      dependencies,
      revisions,
      revisionNextPageToken,
      promotionReceipts,
    ])
      Reflect.deleteProperty(cache, recipeRef);
  }

  async function loadDetail(
    scope: RoleImageResourceScope,
    recipeRef: string,
    showLoading = true,
    revisionPageSize = 20,
  ): Promise<void> {
    const current = ++detailGeneration;
    loadingDetail.value = showLoading;
    problem.value = undefined;
    try {
      const detail = await loadRoleImageDetail(scope, recipeRef);
      if (current !== detailGeneration) return;
      if (
        detail.recipe.ref !== recipeRef ||
        detail.builds.some((build) => build.recipeRef !== recipeRef)
      )
        throw new Error("Invalid role image detail scope");
      assertOwned(scope, detail.recipe);
      for (const build of detail.builds)
        assertOwned(scope, build, detail.recipe.organizationRef);
      for (const artifact of [
        detail.activeArtifact,
        detail.promotionCandidate,
      ]) {
        if (!artifact) continue;
        assertOwned(scope, artifact, detail.recipe.organizationRef);
        if (artifact.recipeRef !== recipeRef)
          throw new Error("Role image artifact recipe mismatch");
      }
      if (!showLoading) {
        recipes[detail.recipe.ref] = detail.recipe;
        builds[detail.recipe.ref] = detail.builds;
        artifacts[detail.recipe.ref] =
          detail.promotionCandidate ?? detail.activeArtifact;
        return;
      }
      const [dependencyItems, revisionPage] = await Promise.all([
        detail.activeArtifact
          ? loadRoleImageDependencies(scope, detail.activeArtifact.ref)
          : Promise.resolve([]),
        loadRoleImageRevisionPage(
          scope,
          recipeRef,
          undefined,
          revisionPageSize,
        ),
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
      if (current === detailGeneration) {
        clearDetail(recipeRef);
        problem.value = asProblem(error);
      }
    } finally {
      if (current === detailGeneration && showLoading)
        loadingDetail.value = false;
    }
  }

  async function loadMoreRevisions(
    scope: RoleImageResourceScope,
    recipeRef: string,
    pageSize = 20,
  ): Promise<void> {
    const pageToken = revisionNextPageToken[recipeRef];
    if (!pageToken || loadingDetail.value) return;
    const current = ++detailGeneration;
    loadingDetail.value = true;
    problem.value = undefined;
    try {
      const page = await loadRoleImageRevisionPage(
        scope,
        recipeRef,
        pageToken,
        pageSize,
      );
      if (current !== detailGeneration) return;
      const merged = new Map(
        (revisions[recipeRef] ?? []).map((item) => [item.ref, item]),
      );
      for (const item of page.items) merged.set(item.ref, item);
      revisions[recipeRef] = [...merged.values()];
      revisionNextPageToken[recipeRef] = page.nextPageToken;
    } catch (error) {
      if (current === detailGeneration) {
        clearDetail(recipeRef);
        problem.value = asProblem(error);
      }
    } finally {
      if (current === detailGeneration) loadingDetail.value = false;
    }
  }

  function roleDefinitionOptions(values: Agent[]): RoleDefinitionOption[] {
    const definitions = new Map<
      string,
      { label: string; agentRefs: Set<string> }
    >();
    for (const agent of values) {
      if (!agent.roleDefinitionRef) continue;
      const current = definitions.get(agent.roleDefinitionRef) ?? {
        label: agent.roleDefinitionName || agent.roleDescription || agent.name,
        agentRefs: new Set<string>(),
      };
      current.agentRefs.add(agent.ref);
      definitions.set(agent.roleDefinitionRef, current);
    }
    return [...definitions.entries()]
      .map(([ref, value]) => ({
        ref,
        label: value.label,
        agentCount: value.agentRefs.size,
      }))
      .sort((left, right) => left.label.localeCompare(right.label));
  }

  function applySupportingCatalogSnapshot(
    agents: Agent[],
    environmentCatalog: RoleEnvironment[],
  ): void {
    roleDefinitions.value = roleDefinitionOptions(agents);
    environments.value = environmentCatalog;
  }

  async function loadSupportingCatalogs(
    scope: RoleImageResourceScope,
    snapshot?: { agents: Agent[]; environments: RoleEnvironment[] },
  ): Promise<void> {
    const projectRef = roleImageScopeKey(scope);
    const resolved = roleImageRuntimeScope(scope);
    const current = ++supportingGeneration;
    supportingController?.abort();
    const controller = new AbortController();
    supportingController = controller;
    createAllowed[projectRef] = false;
    problem.value = undefined;
    if (snapshot)
      applySupportingCatalogSnapshot(snapshot.agents, snapshot.environments);
    try {
      const [definitions, environmentCatalog, canCreate] = await Promise.all([
        snapshot
          ? Promise.resolve(roleDefinitions.value)
          : resolved.kind === "PROJECT"
            ? loadRoleDefinitionOptions(resolved.projectRef, controller.signal)
            : Promise.resolve([]),
        snapshot
          ? Promise.resolve(environments.value)
          : loadRoleEnvironmentCatalog(controller.signal),
        loadRoleImageCreateAccess(
          resolved.kind === "PROJECT" ? resolved.projectRef : undefined,
          controller.signal,
        ),
      ]);
      if (current !== supportingGeneration) return;
      if (!snapshot) {
        roleDefinitions.value = definitions;
        environments.value = environmentCatalog;
      }
      createAllowed[projectRef] = canCreate;
    } catch (error) {
      if (current === supportingGeneration && !controller.signal.aborted)
        problem.value = asProblem(error);
    }
  }

  async function command(
    projectRef: RoleImageResourceScope,
    recipe: RoleImageRecipe,
    action: RoleImageRecipeCommand["action"],
    buildRef?: string,
  ): Promise<void> {
    mutating.value = true;
    problem.value = undefined;
    try {
      assertOwned(projectRef, recipe);
      const receipt = await commandRoleImage(
        projectRef,
        recipe,
        action,
        buildRef,
      );
      assertOwned(projectRef, receipt.recipe);
      recipes[receipt.recipe.ref] = receipt.recipe;
      if (action === "REQUEST_BUILD")
        Reflect.deleteProperty(promotionReceipts, receipt.recipe.ref);
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
    projectRef: RoleImageResourceScope,
    input: Omit<RoleImageRecipeCreateInput, "roleDefinitionRef"> & {
      roleDefinitionRef?: string;
    },
  ): Promise<RoleImageRecipe> {
    mutating.value = true;
    problem.value = undefined;
    try {
      const recipe = await createRoleImage(projectRef, input);
      assertOwned(projectRef, recipe);
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
    projectRef: RoleImageResourceScope,
    recipe: RoleImageRecipe,
    input: RoleImageRecipeUpdateInput,
  ): Promise<RoleImageRecipe> {
    mutating.value = true;
    problem.value = undefined;
    try {
      assertOwned(projectRef, recipe);
      const saved = await updateRoleImage(projectRef, recipe, input);
      assertOwned(projectRef, saved);
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
    projectRef: RoleImageResourceScope,
    recipe: RoleImageRecipe,
    artifact: RoleImageArtifact,
  ): Promise<RoleImagePromotionReceipt> {
    mutating.value = true;
    problem.value = undefined;
    try {
      assertOwned(projectRef, recipe);
      if (
        artifact.scopeKind !== recipe.scopeKind ||
        artifact.organizationRef !== recipe.organizationRef ||
        artifact.recipeRef !== recipe.ref
      )
        throw new Error("Role image promotion artifact owner mismatch");
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
    loadingDetail.value = false;
  }

  watch(
    () => platform.bootstrap?.organizationRef,
    () => {
      dispose();
      for (const cache of [
        recipes,
        builds,
        artifacts,
        revisions,
        revisionNextPageToken,
        promotionReceipts,
        dependencies,
        projectRecipeRefs,
        projectNextPageToken,
        projectTotal,
        createAllowed,
      ])
        for (const key of Object.keys(cache))
          Reflect.deleteProperty(cache, key);
      roleDefinitions.value = [];
      environments.value = [];
      problem.value = undefined;
    },
    { flush: "sync" },
  );

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
    createAllowed,
    environmentByKey,
    roleDefinitionByRef,
    loadingCatalog,
    loadingMore,
    loadingDetail,
    mutating,
    problem,
    catalog,
    loadCatalog,
    applyCatalogSnapshot,
    loadDetail,
    loadMoreRevisions,
    applySupportingCatalogSnapshot,
    loadSupportingCatalogs,
    create,
    update,
    promote,
    command,
    dispose,
  };
});
