import { requestSignal } from "@/shared/api/client";
import {
  commandRoleImageRecipe,
  getRoleImageRecipe,
  listAgents,
  listRoleEnvironments,
  listRoleImageRecipes,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RoleEnvironment,
  RoleImageRecipe,
  RoleImageRecipeCommand,
  RoleImageRecipeCommandReceipt,
  RoleImageRecipeDetail,
  RoleImageRecipePage,
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
