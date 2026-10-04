import {
  assertActiveRuntimeResourceIdentity,
  runtimeResourceOwnerBoundary,
} from "@/features/runtime/active-resource-owner";
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
  queryEffectiveAccess,
  updateRoleImageRecipe,
  listSystemRoleImageRecipes,
  getSystemRoleImageRecipe,
  listSystemRoleImageRecipeRevisions,
  createSystemRoleImageRecipe,
  updateSystemRoleImageRecipe,
  commandSystemRoleImageRecipe,
  promoteSystemRoleImage,
  getSystemAssistant,
  getAgentRuntimeConfiguration,
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
import { csrfToken, mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import {
  roleImageRuntimeScope,
  type RoleImageResourceScope,
} from "./resource-scope";

export interface RoleDefinitionOption {
  ref: string;
  label: string;
  agentCount: number;
}

export async function loadRoleImageCreateAccess(
  projectRef: string | undefined,
  signal: AbortSignal,
): Promise<boolean> {
  return loadRoleImageAccess(projectRef, signal, [
    "image.build",
    "image.source.view",
    "image.source.manage",
  ]);
}

export async function loadRoleImageSourceCreateAccess(
  projectRef: string | undefined,
  signal: AbortSignal,
): Promise<boolean> {
  return loadRoleImageAccess(projectRef, signal, [
    "image.source.view",
    "image.source.manage",
  ]);
}

async function loadRoleImageAccess(
  projectRef: string | undefined,
  signal: AbortSignal,
  permissionKeys: string[],
): Promise<boolean> {
  const page = (
    await unwrap(
      queryEffectiveAccess({
        body: {
          target: projectRef
            ? { kind: "PROJECT", projectRef }
            : { kind: "ORGANIZATION" },
          permissionKeys,
        },
        headers: { "X-CSRF-Token": csrfToken() },
        signal,
      }),
    )
  ).data;
  return permissionKeys.every((key) => {
    const decisions = page.items.filter((item) => item.permissionKey === key);
    const decision = decisions[0];
    return (
      decisions.length === 1 &&
      decision?.decision === "ALLOWED" &&
      decision.target.kind === (projectRef ? "PROJECT" : "ORGANIZATION") &&
      decision.target.projectRef === projectRef &&
      !decision.target.resourceKind &&
      !decision.target.resourceRef
    );
  });
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
  scope: RoleImageResourceScope,
  pageToken?: string,
  signal: AbortSignal = requestSignal(),
  filter: {
    query?: string;
    state?: "ACTIVE" | "ARCHIVED";
    roleDefinitionRef?: string;
  } = {},
  pageSize = 20,
): Promise<RoleImageRecipePage> {
  const owner = runtimeResourceOwnerBoundary(scope);
  if (new TextEncoder().encode(filter.query ?? "").length > 128)
    throw new Error("Role image query exceeds 128 UTF-8 bytes");
  const resolved = roleImageRuntimeScope(scope);
  const query = { pageSize, ...(pageToken ? { pageToken } : {}), ...filter };
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (await unwrap(listSystemRoleImageRecipes({ query, signal }))).data,
      (value) => value.items,
    );
  const projectRef = resolved.projectRef;
  return checkedRoleImageReadback(
    owner,
    (
      await unwrap(
        listRoleImageRecipes({
          path: { projectRef },
          query: {
            pageSize,
            ...(pageToken ? { pageToken } : {}),
            ...filter,
          },
          signal,
        }),
      )
    ).data,
    (value) => value.items,
  );
}

export async function loadRoleImageDetail(
  scope: RoleImageResourceScope,
  recipeRef: string,
  signal: AbortSignal = requestSignal(),
): Promise<RoleImageRecipeDetail> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (await unwrap(getSystemRoleImageRecipe({ path: { recipeRef }, signal })))
        .data,
      (value) => [
        value.recipe,
        ...value.builds,
        ...(value.activeArtifact ? [value.activeArtifact] : []),
        ...(value.promotionCandidate ? [value.promotionCandidate] : []),
      ],
    );
  const projectRef = resolved.projectRef;
  return checkedRoleImageReadback(
    owner,
    (
      await unwrap(
        getRoleImageRecipe({
          path: { projectRef, recipeRef },
          signal,
        }),
      )
    ).data,
    (value) => [
      value.recipe,
      ...value.builds,
      ...(value.activeArtifact ? [value.activeArtifact] : []),
      ...(value.promotionCandidate ? [value.promotionCandidate] : []),
    ],
  );
}

export async function loadRoleImageRevisionPage(
  scope: RoleImageResourceScope,
  recipeRef: string,
  pageToken?: string,
  pageSize = 40,
): Promise<RoleImageRecipeRevisionPage> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (
        await unwrap(
          listSystemRoleImageRecipeRevisions({
            path: { recipeRef },
            query: { pageSize, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(),
          }),
        )
      ).data,
      () => [],
    );
  const projectRef = resolved.projectRef;
  return checkedRoleImageReadback(
    owner,
    (
      await unwrap(
        listRoleImageRecipeRevisions({
          path: { projectRef, recipeRef },
          query: {
            pageSize,
            ...(pageToken ? { pageToken } : {}),
          },
          signal: requestSignal(),
        }),
      )
    ).data,
    () => [],
  );
}

export async function createRoleImage(
  scope: RoleImageResourceScope,
  input: Omit<RoleImageRecipeCreateInput, "roleDefinitionRef"> & {
    roleDefinitionRef?: string;
  },
): Promise<RoleImageRecipe> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (
        await mutate((headers) =>
          createSystemRoleImageRecipe({
            body: { name: input.name, environment: input.environment },
            headers: {
              "Idempotency-Key": headers["Idempotency-Key"],
              "X-CSRF-Token": headers["X-CSRF-Token"],
            },
            signal: requestSignal(),
          }),
        )
      ).data,
      (value) => [value],
    );
  if (!input.roleDefinitionRef)
    throw new Error("Project role definition is required");
  const projectRef = resolved.projectRef;
  const body: RoleImageRecipeCreateInput = {
    ...input,
    roleDefinitionRef: input.roleDefinitionRef,
  };
  return checkedRoleImageReadback(
    owner,
    (
      await mutate((headers) =>
        createRoleImageRecipe({
          path: { projectRef },
          body,
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
          },
          signal: requestSignal(),
        }),
      )
    ).data,
    (value) => [value],
  );
}

export async function updateRoleImage(
  scope: RoleImageResourceScope,
  recipe: RoleImageRecipe,
  input: RoleImageRecipeUpdateInput,
): Promise<RoleImageRecipe> {
  const owner = runtimeResourceOwnerBoundary(scope);
  owner.assert(recipe);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (
        await mutate(
          (headers) =>
            updateSystemRoleImageRecipe({
              path: { recipeRef: recipe.ref },
              body: input,
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          recipe.version,
        )
      ).data,
      (value) => [value],
    );
  const projectRef = resolved.projectRef;
  return checkedRoleImageReadback(
    owner,
    (
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
    ).data,
    (value) => [value],
  );
}

export async function loadRoleImageDependencies(
  scope: RoleImageResourceScope,
  imageArtifactRef: string,
): Promise<RuntimeEnvironmentSet[]> {
  const owner = runtimeResourceOwnerBoundary(scope);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION") {
    const assistant = (
      await unwrap(getSystemAssistant({ signal: requestSignal() }))
    ).data;
    if (!assistant.ref)
      throw new Error("System assistant locator is unavailable");
    const configuration = (
      await unwrap(
        getAgentRuntimeConfiguration({
          path: { agentRef: assistant.ref },
          signal: requestSignal(),
        }),
      )
    ).data;
    if (
      configuration.configuration.agentRef !== assistant.ref ||
      configuration.environmentBinding.agentRef !== assistant.ref ||
      configuration.environmentBinding.environmentRef !==
        configuration.environment.ref
    )
      throw new Error("System assistant runtime locator mismatch");
    const environment = configuration.environment;
    owner.assert(environment);
    assertActiveRuntimeResourceIdentity(
      scope,
      environment,
      resolved.organizationRef,
    );
    if (environment.currentVersion.image.artifactRef !== imageArtifactRef)
      return [];
    return [environment];
  }
  const projectRef = resolved.projectRef;
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
    owner.assertCurrent();
    for (const environment of page.items) owner.assert(environment);
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

export async function loadRoleEnvironmentCatalog(
  signal: AbortSignal = requestSignal(),
): Promise<RoleEnvironment[]> {
  return (await unwrap(listRoleEnvironments({ signal }))).data.items;
}

export async function loadRoleDefinitionOptions(
  projectRef: string,
  signal: AbortSignal = requestSignal(),
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
          signal,
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
  scope: RoleImageResourceScope,
  recipe: RoleImageRecipe,
  action: RoleImageRecipeCommand["action"],
  buildRef?: string,
): Promise<RoleImageRecipeCommandReceipt> {
  const owner = runtimeResourceOwnerBoundary(scope);
  owner.assert(recipe);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (
        await mutate(
          (headers) =>
            commandSystemRoleImageRecipe({
              path: { recipeRef: recipe.ref },
              body: { action, ...(buildRef ? { buildRef } : {}) },
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          recipe.version,
        )
      ).data,
      (value) => [
        value.recipe,
        ...(value.imageBuild ? [value.imageBuild] : []),
      ],
    );
  const projectRef = resolved.projectRef;
  return checkedRoleImageReadback(
    owner,
    (
      await mutate(
        (headers) =>
          commandRoleImageRecipe({
            path: { projectRef, recipeRef: recipe.ref },
            body: { action, ...(buildRef ? { buildRef } : {}) },
            headers: versionedHeaders(headers),
            signal: requestSignal(),
          }),
        recipe.version,
      )
    ).data,
    (value) => [value.recipe, ...(value.imageBuild ? [value.imageBuild] : [])],
  );
}

export async function promoteRoleImageArtifact(
  scope: RoleImageResourceScope,
  recipe: RoleImageRecipe,
  imageArtifactRef: string,
  expectedProvenanceSha256: string,
): Promise<RoleImagePromotionReceipt> {
  const owner = runtimeResourceOwnerBoundary(scope);
  owner.assert(recipe);
  const resolved = roleImageRuntimeScope(scope);
  if (resolved.kind === "ORGANIZATION")
    return checkedRoleImageReadback(
      owner,
      (
        await mutate(
          (headers) =>
            promoteSystemRoleImage({
              path: { recipeRef: recipe.ref },
              body: { imageArtifactRef, expectedProvenanceSha256 },
              headers: versionedHeaders(headers),
              signal: requestSignal(),
            }),
          recipe.version,
        )
      ).data,
      () => [],
    );
  const projectRef = resolved.projectRef;
  return checkedRoleImageReadback(
    owner,
    (
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
    ).data,
    () => [],
  );
}

function checkedRoleImageReadback<T>(
  owner: ReturnType<typeof runtimeResourceOwnerBoundary>,
  value: T,
  resources: (
    value: T,
  ) => import("@/features/runtime/resource-scope").RuntimeScopedResourceIdentity[],
): T {
  owner.assertCurrent();
  for (const resource of resources(value)) owner.assert(resource);
  return value;
}
