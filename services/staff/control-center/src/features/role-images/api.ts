import { requestSignal } from "@/shared/api/client";
import {
  commandRoleImageRecipe,
  createRoleImageRecipe,
  getRoleImageRecipe,
  listAgents,
  listRoleEnvironments,
  listRoleImageRecipeRevisions,
  listRoleImageRecipes,
  listRuntimeEnvironmentSets,
  promoteRoleImage,
  updateRoleImageRecipe,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RoleEnvironment,
  RoleImageRecipe,
  RoleImageRecipeCommand,
  RoleImageRecipeCommandReceipt,
  RoleImageRecipeCreateInput,
  RoleImageRecipeDetail,
  RoleImageRecipePage,
  RoleImageRecipeRevisionPage,
  RoleImageRecipeUpdateInput,
  RoleImagePromotionReceipt,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export interface RoleDefinitionOption {
  ref: string;
  label: string;
  agentCount: number;
}

function versionedHeaders(headers: MutationHeaders): {
  "Idempotency-Key": string;
  "If-Match": string;
  "X-CSRF-Token": string;
} {
  if (!headers["If-Match"])
    throw new Error("Role image version header is unavailable");
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "If-Match": headers["If-Match"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

export async function loadRoleImagePage(
  projectRef: string,
  pageToken?: string,
): Promise<RoleImageRecipePage> {
  return (
    await unwrap(
      listRoleImageRecipes({
        path: { projectRef },
        query: {
          pageSize: 40,
          ...(pageToken ? { pageToken } : {}),
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function loadRoleImageDetail(
  projectRef: string,
  recipeRef: string,
): Promise<RoleImageRecipeDetail> {
  return (
    await unwrap(
      getRoleImageRecipe({
        path: { projectRef, recipeRef },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function loadRoleImageRevisionPage(
  projectRef: string,
  recipeRef: string,
  pageToken?: string,
): Promise<RoleImageRecipeRevisionPage> {
  return (
    await unwrap(
      listRoleImageRecipeRevisions({
        path: { projectRef, recipeRef },
        query: {
          pageSize: 40,
          ...(pageToken ? { pageToken } : {}),
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function createRoleImage(
  projectRef: string,
  input: RoleImageRecipeCreateInput,
): Promise<RoleImageRecipe> {
  return (
    await mutate((headers) =>
      createRoleImageRecipe({
        path: { projectRef },
        body: input,
        headers: {
          "Idempotency-Key": headers["Idempotency-Key"],
          "X-CSRF-Token": headers["X-CSRF-Token"],
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function updateRoleImage(
  projectRef: string,
  recipe: RoleImageRecipe,
  input: RoleImageRecipeUpdateInput,
): Promise<RoleImageRecipe> {
  return (
    await mutate(
      (headers) =>
        updateRoleImageRecipe({
          path: { projectRef, recipeRef: recipe.ref },
          body: input,
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      recipe.version,
    )
  ).data;
}

export async function loadRoleImageDependencies(
  projectRef: string,
  imageArtifactRef: string,
): Promise<RuntimeEnvironmentSet[]> {
  const result: RuntimeEnvironmentSet[] = [];
  const visitedTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = (
      await unwrap(
        listRuntimeEnvironmentSets({
          path: { projectRef },
          query: {
            pageSize: 100,
            ...(pageToken ? { pageToken } : {}),
          },
          signal: requestSignal(),
        }),
      )
    ).data;
    result.push(
      ...page.items.filter(
        (environment) =>
          environment.currentVersion.image.artifactRef === imageArtifactRef,
      ),
    );
    pageToken = page.nextPageToken;
    if (pageToken && visitedTokens.has(pageToken))
      throw new Error("Runtime environment catalog returned a repeated token");
    if (pageToken) visitedTokens.add(pageToken);
  } while (pageToken);
  return result;
}

export async function loadRoleEnvironmentCatalog(): Promise<RoleEnvironment[]> {
  return (await unwrap(listRoleEnvironments({ signal: requestSignal() }))).data
    .items;
}

export async function loadRoleDefinitionOptions(
  projectRef: string,
): Promise<RoleDefinitionOption[]> {
  const values = new Map<string, { label: string; agentRefs: Set<string> }>();
  const visitedTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = (
      await unwrap(
        listAgents({
          path: { projectRef },
          query: {
            pageSize: 100,
            ...(pageToken ? { pageToken } : {}),
          },
          signal: requestSignal(),
        }),
      )
    ).data;
    for (const agent of page.items) {
      if (!agent.roleDefinitionRef) continue;
      const current = values.get(agent.roleDefinitionRef) ?? {
        label: agent.roleDefinitionName || agent.roleDescription || agent.name,
        agentRefs: new Set<string>(),
      };
      current.agentRefs.add(agent.ref);
      values.set(agent.roleDefinitionRef, current);
    }
    pageToken = page.nextPageToken;
    if (pageToken && visitedTokens.has(pageToken))
      throw new Error("Agent catalog returned a repeated page token");
    if (pageToken) visitedTokens.add(pageToken);
  } while (pageToken);

  return [...values.entries()]
    .map(([ref, value]) => ({
      ref,
      label: value.label,
      agentCount: value.agentRefs.size,
    }))
    .sort((left, right) => left.label.localeCompare(right.label, "ru"));
}

export async function commandRoleImage(
  projectRef: string,
  recipe: RoleImageRecipe,
  action: RoleImageRecipeCommand["action"],
): Promise<RoleImageRecipeCommandReceipt> {
  return (
    await mutate(
      (headers) =>
        commandRoleImageRecipe({
          path: { projectRef, recipeRef: recipe.ref },
          body: { action },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      recipe.version,
    )
  ).data;
}

export async function promoteRoleImageArtifact(
  projectRef: string,
  recipe: RoleImageRecipe,
  imageArtifactRef: string,
  expectedProvenanceSha256: string,
): Promise<RoleImagePromotionReceipt> {
  return (
    await mutate(
      (headers) =>
        promoteRoleImage({
          path: { projectRef, recipeRef: recipe.ref },
          body: { imageArtifactRef, expectedProvenanceSha256 },
          headers: versionedHeaders(headers),
          signal: requestSignal(),
        }),
      recipe.version,
    )
  ).data;
}
