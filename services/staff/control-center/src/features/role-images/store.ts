import { defineStore } from "pinia";
import { computed, reactive, ref } from "vue";

import {
  commandImageBuild,
  commandRoleImage,
  fetchRoleImageBuild,
  fetchRoleImageRecipe,
} from "@/shared/api/adapters/role-images";
import { fetchResources } from "@/shared/api/adapters/resources";
import type {
  ManageImageBuild,
  ManageRoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import {
  beginRequest,
  failRequest,
  finishRequest,
  invalidate,
  remoteState,
  resetRemoteState,
} from "@/shared/lib/remote";
import {
  type RoleImageInputModel,
  type RoleImageRecipeDetailModel,
  type RoleImageResourceModel,
  toRoleImageInput,
  toRoleImageRecipeDetailModel,
  toRoleImageResourceModel,
} from "./model";

export const useRoleImagesStore = defineStore("role-images", () => {
  const resources = reactive(remoteState<RoleImageResourceModel[]>([]));
  const mutationProblem = ref<AppProblem | null>(null);
  const mutating = ref(false);
  let mutationVersion = 0;
  const recipeDetail = reactive(
    remoteState<RoleImageRecipeDetailModel | null>(null),
  );
  const buildDetail = reactive(
    remoteState<RoleImageResourceModel | null>(null),
  );
  const recipes = computed(() =>
    resources.data.filter((item) => item.kind === "ROLE_IMAGE_RECIPE"),
  );
  const builds = computed(() =>
    resources.data.filter((item) => item.kind === "IMAGE_BUILD"),
  );

  async function load(): Promise<void> {
    const request = beginRequest(resources);
    try {
      const [recipePage, buildPage] = await Promise.all([
        fetchResources("ROLE_IMAGE_RECIPE"),
        fetchResources("IMAGE_BUILD"),
      ]);
      const items = [...recipePage.resources, ...buildPage.resources];
      finishRequest(
        resources,
        request,
        items.map(toRoleImageResourceModel),
        items.length === 0,
      );
    } catch (error) {
      failRequest(resources, request, asProblem(error));
    }
  }

  async function mutate(operation: () => Promise<unknown>): Promise<boolean> {
    const version = ++mutationVersion;
    invalidate(resources);
    invalidate(recipeDetail);
    invalidate(buildDetail);
    mutationProblem.value = null;
    mutating.value = true;
    try {
      await operation();
      if (version !== mutationVersion) return false;
      await load();
      if (version !== mutationVersion) return false;
      return true;
    } catch (error) {
      if (version !== mutationVersion) return false;
      mutationProblem.value = asProblem(error);
      if (mutationProblem.value.kind === "conflict")
        resources.phase = "conflict";
      return false;
    } finally {
      if (version === mutationVersion) mutating.value = false;
    }
  }

  const saveRecipe = (
    resource: RoleImageResourceModel | null,
    name: string,
    input: RoleImageInputModel,
  ) =>
    mutate(() =>
      commandRoleImage(
        resource
          ? {
              action: "UPDATE",
              recipeId: resource.id,
              name,
              input: toRoleImageInput(input),
            }
          : { action: "CREATE", name, input: toRoleImageInput(input) },
        resource?.version,
      ),
    );
  const executeRecipeAction = (
    resource: RoleImageResourceModel,
    action: Exclude<ManageRoleImageRecipe["action"], "CREATE" | "UPDATE">,
  ) =>
    mutate(() =>
      commandRoleImage({ action, recipeId: resource.id }, resource.version),
    );
  const commandBuild = (
    resource: RoleImageResourceModel,
    action: ManageImageBuild["action"],
  ) =>
    mutate(() =>
      commandImageBuild(
        { imageBuildId: resource.id, action },
        resource.version,
      ),
    );

  async function loadRecipeDetail(
    resource: RoleImageResourceModel,
  ): Promise<void> {
    const request = beginRequest(recipeDetail);
    try {
      const data = await fetchRoleImageRecipe(resource.id, resource.version);
      finishRequest(
        recipeDetail,
        request,
        toRoleImageRecipeDetailModel(data),
        false,
      );
    } catch (error) {
      failRequest(recipeDetail, request, asProblem(error));
    }
  }

  async function loadBuildDetail(
    resource: RoleImageResourceModel,
  ): Promise<void> {
    const request = beginRequest(buildDetail);
    try {
      const data = await fetchRoleImageBuild(resource.id, resource.version);
      finishRequest(
        buildDetail,
        request,
        toRoleImageResourceModel(data),
        false,
      );
    } catch (error) {
      failRequest(buildDetail, request, asProblem(error));
    }
  }

  function reset(): void {
    mutationVersion += 1;
    resetRemoteState(resources, []);
    resetRemoteState(recipeDetail, null);
    resetRemoteState(buildDetail, null);
    mutationProblem.value = null;
    mutating.value = false;
  }

  return {
    resources,
    recipes,
    builds,
    recipeDetail,
    buildDetail,
    mutationProblem,
    mutating,
    load,
    loadRecipeDetail,
    loadBuildDetail,
    saveRecipe,
    executeRecipeAction,
    commandBuild,
    reset,
  };
});

export type RoleImageAction = Exclude<
  ManageRoleImageRecipe["action"],
  "CREATE" | "UPDATE"
>;
