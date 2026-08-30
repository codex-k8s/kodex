import { defineStore } from "pinia";
import { reactive } from "vue";

import { requestSignal } from "@/shared/api/client";
import {
  bindAgentRuntimeEnvironment,
  createConfigOverlayDraft,
  createRuntimeEnvironmentSet,
  deleteRuntimeEnvironment,
  getAgentRuntimeConfiguration,
  getRoleImageRecipe,
  getRuntimeEnvironmentReadiness,
  getRuntimeEnvironmentSet,
  listAgentRuntimeConfigurationVersions,
  listRuntimeEnvironmentAgents,
  listRuntimeEnvironmentSets,
  listRuntimeEnvironmentVersions,
  listRoleImageRecipes,
  publishAgentRuntimeConfiguration,
  publishConfigOverlayDraft,
  publishRuntimeEnvironmentVersion,
  rollbackConfigOverlay,
  rollbackRuntimeEnvironment,
  setRuntimeEnvironmentEnabled,
  validateConfigOverlayDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AgentRuntimeConfiguration,
  AgentRuntimeConfigurationInput,
  AgentRuntimeConfigurationView,
  Agent,
  AgentPage,
  RuntimeEnvironmentInput,
  RuntimeEnvironmentPage,
  RuntimeEnvironmentReadiness,
  RuntimeEnvironmentSet,
  RuntimeEnvironmentVersion,
  RoleImageArtifact,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { asProblem, type AppProblem, unwrap } from "@/shared/api/problem";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";

export interface PromotedRoleImageOption extends AsyncEntityOption {
  recipeRef: string;
  artifactRef: string;
  generation: number;
  promotedReference: string;
}

type RuntimeResource =
  | `agent:${string}`
  | `agent-versions:${string}`
  | `environment:${string}`
  | `environment-agents:${string}`
  | `environment-readiness:${string}`
  | `environment-versions:${string}`;

function versionHeaders(headers: MutationHeaders): {
  "If-Match": string;
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  const version = headers["If-Match"];
  if (!version) throw new Error("Runtime resource version is unavailable");
  return {
    "If-Match": version,
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

export const useRuntimeStore = defineStore("runtime-configuration", () => {
  const agentViews = reactive<Record<string, AgentRuntimeConfigurationView>>(
    {},
  );
  const agentVersions = reactive<Record<string, AgentRuntimeConfiguration[]>>(
    {},
  );
  const environments = reactive<Record<string, RuntimeEnvironmentSet>>({});
  const environmentReadiness = reactive<
    Record<string, RuntimeEnvironmentReadiness | undefined>
  >({});
  const environmentAgents = reactive<Record<string, Agent[]>>({});
  const environmentAgentCursors = reactive<Record<string, string | undefined>>(
    {},
  );
  const environmentVersions = reactive<
    Record<string, RuntimeEnvironmentVersion[]>
  >({});
  const environmentVersionCursors = reactive<
    Record<string, string | undefined>
  >({});
  const loading = reactive<Record<string, boolean>>({});
  const problems = reactive<Record<string, AppProblem | undefined>>({});
  const generations = new Map<string, number>();

  async function query<T>(
    key: RuntimeResource,
    request: () => Promise<T>,
    apply: (value: T) => void,
  ): Promise<void> {
    const generation = (generations.get(key) ?? 0) + 1;
    generations.set(key, generation);
    loading[key] = true;
    problems[key] = undefined;
    try {
      const value = await request();
      if (generations.get(key) === generation) apply(value);
    } catch (error) {
      if (generations.get(key) === generation) problems[key] = asProblem(error);
    } finally {
      if (generations.get(key) === generation) loading[key] = false;
    }
  }

  function applyAgentView(
    agentRef: string,
    view: AgentRuntimeConfigurationView,
  ): AgentRuntimeConfigurationView {
    agentViews[agentRef] = view;
    environments[view.environment.ref] = view.environment;
    return view;
  }

  async function loadAgentRuntime(agentRef: string): Promise<void> {
    await query(
      `agent:${agentRef}`,
      async () =>
        (
          await unwrap(
            getAgentRuntimeConfiguration({
              path: { agentRef },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => applyAgentView(agentRef, value),
    );
  }

  async function loadAgentVersions(agentRef: string): Promise<void> {
    await query(
      `agent-versions:${agentRef}`,
      async () =>
        (
          await unwrap(
            listAgentRuntimeConfigurationVersions({
              path: { agentRef },
              query: { pageSize: 100 },
              signal: requestSignal(),
            }),
          )
        ).data.items,
      (values) => {
        agentVersions[agentRef] = values;
      },
    );
  }

  async function saveAgentRuntime(
    agentRef: string,
    input: AgentRuntimeConfigurationInput,
    agentVersion: number,
  ): Promise<AgentRuntimeConfigurationView> {
    const result = await mutate(
      (headers) =>
        publishAgentRuntimeConfiguration({
          path: { agentRef },
          body: input,
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    );
    return applyAgentView(agentRef, result.data);
  }

  async function saveOverlayDraft(
    agentRef: string,
    content: string,
    agentVersion: number,
  ): Promise<AgentRuntimeConfigurationView> {
    const result = await mutate(
      (headers) =>
        createConfigOverlayDraft({
          path: { agentRef },
          body: { content },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    );
    return applyAgentView(agentRef, result.data);
  }

  async function changeOverlay(
    agentRef: string,
    action: "VALIDATE" | "PUBLISH",
    agentVersion: number,
  ): Promise<AgentRuntimeConfigurationView> {
    const request =
      action === "VALIDATE"
        ? validateConfigOverlayDraft
        : publishConfigOverlayDraft;
    const result = await mutate(
      (headers) =>
        request({
          path: { agentRef },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    );
    return applyAgentView(agentRef, result.data);
  }

  async function restoreOverlay(
    agentRef: string,
    publishedOverlayRef: string,
    agentVersion: number,
  ): Promise<AgentRuntimeConfigurationView> {
    const result = await mutate(
      (headers) =>
        rollbackConfigOverlay({
          path: { agentRef },
          body: { publishedOverlayRef },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    );
    return applyAgentView(agentRef, result.data);
  }

  async function bindEnvironment(
    agentRef: string,
    environmentRef: string,
    agentVersion: number,
  ): Promise<AgentRuntimeConfigurationView> {
    const result = await mutate(
      (headers) =>
        bindAgentRuntimeEnvironment({
          path: { agentRef },
          body: { environmentRef },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    );
    return applyAgentView(agentRef, result.data);
  }

  async function searchEnvironmentPage(
    projectRef: string,
    search: string,
    pageToken?: string,
  ): Promise<RuntimeEnvironmentPage> {
    return (
      await unwrap(
        listRuntimeEnvironmentSets({
          path: { projectRef },
          query: {
            ...(search.trim() ? { query: search.trim() } : {}),
            ...(pageToken ? { pageToken } : {}),
            pageSize: 30,
          },
          signal: requestSignal(),
        }),
      )
    ).data;
  }

  async function searchPromotedRoleImagePage(
    projectRef: string,
    search: string,
    pageToken?: string,
    signal?: AbortSignal,
  ): Promise<AsyncEntityOptionPage> {
    const needle = search.trim().toLocaleLowerCase();
    const visitedTokens = new Set<string>();
    let cursor = pageToken;
    do {
      const page = (
        await unwrap(
          listRoleImageRecipes({
            path: { projectRef },
            query: {
              pageSize: 30,
              ...(cursor ? { pageToken: cursor } : {}),
            },
            signal: signal ?? requestSignal(),
          }),
        )
      ).data;
      const options: PromotedRoleImageOption[] = page.items.flatMap(
        (recipe) => {
          const artifactRef = recipe.activeImageArtifactRef;
          if (
            recipe.state !== "ACTIVE" ||
            !recipe.promotedImageReady ||
            !artifactRef ||
            (needle &&
              !recipe.name.toLocaleLowerCase().includes(needle) &&
              !recipe.promotedImageReference
                ?.toLocaleLowerCase()
                .includes(needle))
          )
            return [];
          return [
            {
              ref: artifactRef,
              title: recipe.name,
              description: recipe.promotedImageReference,
              meta: `generation ${String(recipe.generation)}`,
              recipeRef: recipe.ref,
              artifactRef,
              generation: recipe.generation,
              promotedReference: recipe.promotedImageReference ?? "",
            },
          ];
        },
      );
      if (options.length || !page.nextPageToken)
        return { items: options, nextPageToken: page.nextPageToken };
      cursor = page.nextPageToken;
      if (visitedTokens.has(cursor))
        throw new Error("Role image catalog returned a repeated page token");
      visitedTokens.add(cursor);
    } while (cursor);
    return { items: [] };
  }

  async function loadPromotedRoleImageArtifact(
    projectRef: string,
    recipeRef: string,
    expectedArtifactRef: string,
  ): Promise<RoleImageArtifact> {
    const detail = (
      await unwrap(
        getRoleImageRecipe({
          path: { projectRef, recipeRef },
          signal: requestSignal(),
        }),
      )
    ).data;
    if (
      !detail.activeArtifact ||
      detail.activeArtifact.ref !== expectedArtifactRef ||
      detail.activeArtifact.admissionVerdict !== "ACCEPTED"
    )
      throw new Error("Promoted role image artifact is unavailable");
    return detail.activeArtifact;
  }

  async function loadEnvironment(environmentRef: string): Promise<void> {
    await query(
      `environment:${environmentRef}`,
      async () =>
        (
          await unwrap(
            getRuntimeEnvironmentSet({
              path: { environmentRef },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        environments[value.ref] = value;
      },
    );
  }

  async function loadEnvironmentReadiness(
    environmentRef: string,
  ): Promise<void> {
    await query(
      `environment-readiness:${environmentRef}`,
      async () =>
        (
          await unwrap(
            getRuntimeEnvironmentReadiness({
              path: { environmentRef },
              signal: requestSignal(),
            }),
          )
        ).data,
      (value) => {
        environmentReadiness[environmentRef] = value;
      },
    );
  }

  async function loadEnvironmentAgents(
    environmentRef: string,
    reset = true,
  ): Promise<void> {
    const pageToken = reset
      ? undefined
      : environmentAgentCursors[environmentRef];
    if (!reset && !pageToken) return;
    await query(
      `environment-agents:${environmentRef}`,
      async (): Promise<AgentPage> =>
        (
          await unwrap(
            listRuntimeEnvironmentAgents({
              path: { environmentRef },
              query: {
                pageSize: 30,
                ...(pageToken ? { pageToken } : {}),
              },
              signal: requestSignal(),
            }),
          )
        ).data,
      (page) => {
        if (reset) environmentAgents[environmentRef] = page.items;
        else {
          const merged = new Map(
            (environmentAgents[environmentRef] ?? []).map((item) => [
              item.ref,
              item,
            ]),
          );
          for (const item of page.items) merged.set(item.ref, item);
          environmentAgents[environmentRef] = [...merged.values()];
        }
        environmentAgentCursors[environmentRef] = page.nextPageToken;
      },
    );
  }

  async function loadEnvironmentVersions(
    environmentRef: string,
    reset = true,
  ): Promise<void> {
    const pageToken = reset
      ? undefined
      : environmentVersionCursors[environmentRef];
    if (!reset && !pageToken) return;
    await query(
      `environment-versions:${environmentRef}`,
      async () =>
        (
          await unwrap(
            listRuntimeEnvironmentVersions({
              path: { environmentRef },
              query: {
                pageSize: 30,
                ...(pageToken ? { pageToken } : {}),
              },
              signal: requestSignal(),
            }),
          )
        ).data,
      (page) => {
        if (reset) environmentVersions[environmentRef] = page.items;
        else {
          const merged = new Map(
            (environmentVersions[environmentRef] ?? []).map((item) => [
              item.ref,
              item,
            ]),
          );
          for (const item of page.items) merged.set(item.ref, item);
          environmentVersions[environmentRef] = [...merged.values()];
        }
        environmentVersionCursors[environmentRef] = page.nextPageToken;
      },
    );
  }

  async function createEnvironment(
    projectRef: string,
    input: RuntimeEnvironmentInput,
  ): Promise<RuntimeEnvironmentSet> {
    const result = await mutate((headers) =>
      createRuntimeEnvironmentSet({
        path: { projectRef },
        body: input,
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    );
    environments[result.data.ref] = result.data;
    return result.data;
  }

  async function publishEnvironment(
    current: RuntimeEnvironmentSet,
    input: RuntimeEnvironmentInput,
  ): Promise<RuntimeEnvironmentSet> {
    const result = await mutate(
      (headers) =>
        publishRuntimeEnvironmentVersion({
          path: { environmentRef: current.ref },
          body: input,
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      current.version,
    );
    environments[result.data.ref] = result.data;
    return result.data;
  }

  async function restoreEnvironment(
    current: RuntimeEnvironmentSet,
    publishedVersionRef: string,
  ): Promise<RuntimeEnvironmentSet> {
    const result = await mutate(
      (headers) =>
        rollbackRuntimeEnvironment({
          path: { environmentRef: current.ref },
          body: { publishedVersionRef },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      current.version,
    );
    environments[result.data.ref] = result.data;
    return result.data;
  }

  async function setEnvironmentEnabled(
    current: RuntimeEnvironmentSet,
    enabled: boolean,
  ): Promise<RuntimeEnvironmentSet> {
    const result = await mutate(
      (headers) =>
        setRuntimeEnvironmentEnabled({
          path: { environmentRef: current.ref },
          body: { enabled },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      current.version,
    );
    environments[result.data.ref] = result.data;
    await loadEnvironmentReadiness(result.data.ref);
    return result.data;
  }

  async function removeEnvironment(
    current: RuntimeEnvironmentSet,
  ): Promise<RuntimeEnvironmentSet> {
    const result = await mutate(
      (headers) =>
        deleteRuntimeEnvironment({
          path: { environmentRef: current.ref },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      current.version,
    );
    environments[result.data.ref] = result.data;
    return result.data;
  }

  function clear(): void {
    generations.clear();
    for (const target of [
      agentViews,
      agentVersions,
      environments,
      environmentReadiness,
      environmentAgents,
      environmentAgentCursors,
      environmentVersions,
      environmentVersionCursors,
      loading,
      problems,
    ]) {
      for (const key of Object.keys(target))
        Reflect.deleteProperty(target, key);
    }
  }

  return {
    agentViews,
    agentVersions,
    environments,
    environmentReadiness,
    environmentAgents,
    environmentAgentCursors,
    environmentVersions,
    environmentVersionCursors,
    loading,
    problems,
    loadAgentRuntime,
    loadAgentVersions,
    saveAgentRuntime,
    saveOverlayDraft,
    changeOverlay,
    restoreOverlay,
    bindEnvironment,
    searchEnvironmentPage,
    searchPromotedRoleImagePage,
    loadPromotedRoleImageArtifact,
    loadEnvironment,
    loadEnvironmentReadiness,
    loadEnvironmentAgents,
    loadEnvironmentVersions,
    createEnvironment,
    publishEnvironment,
    restoreEnvironment,
    setEnvironmentEnabled,
    removeEnvironment,
    clear,
  };
});
