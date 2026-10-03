import type {
  RoleImageArtifact,
  RuntimeEnvironmentImage,
  RuntimeEnvironmentTool,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import type { RuntimeResourceScope } from "./resource-scope";

export interface RuntimeImageOption extends AsyncEntityOption {
  recipeRef: string;
  generation: number;
}

export interface RuntimeImageCatalog {
  loadPage(
    scope: RuntimeResourceScope,
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
    pageSize?: number,
  ): Promise<AsyncEntityOptionPage>;
  loadArtifact(
    scope: RuntimeResourceScope,
    recipeRef: string,
    artifactRef: string,
    signal: AbortSignal,
  ): Promise<{ artifact: RoleImageArtifact; recipeName: string }>;
}

export function assertPromotedRuntimeImage(
  artifact: RoleImageArtifact,
  expected: Pick<
    RuntimeEnvironmentImage,
    "artifactRef" | "recipeRef" | "recipeGeneration"
  >,
): void {
  const digest = artifact.manifestDigest.replace(/^sha256:/, "");
  if (
    artifact.ref !== expected.artifactRef ||
    artifact.recipeRef !== expected.recipeRef ||
    artifact.recipeGeneration !== expected.recipeGeneration ||
    artifact.admissionVerdict !== "ACCEPTED" ||
    artifact.promotionState !== "PROMOTED" ||
    !/^[a-f0-9]{64}$/.test(digest) ||
    !artifact.promotedReference?.endsWith(`@sha256:${digest}`)
  )
    throw new AppProblem({
      status: 409,
      code: "IMAGE_ARTIFACT_NOT_CURRENT",
      retryable: false,
      kind: "conflict",
    });
}

export function toolsForRuntimeImage(
  tools: readonly RuntimeEnvironmentTool[],
  artifact: RoleImageArtifact,
): RuntimeEnvironmentTool[] {
  const commands = new Set(artifact.tools.map((tool) => tool.name));
  return tools
    .filter((tool) => commands.has(tool.command))
    .map((tool) => ({ ...tool }));
}

export function runtimeImageOption(
  value: AsyncEntityOption,
): RuntimeImageOption {
  if (
    !("recipeRef" in value) ||
    typeof value.recipeRef !== "string" ||
    !value.recipeRef ||
    !("generation" in value) ||
    typeof value.generation !== "number" ||
    !Number.isSafeInteger(value.generation) ||
    value.generation < 1
  )
    throw new Error("Runtime image catalog identity is invalid");
  return { ...value, recipeRef: value.recipeRef, generation: value.generation };
}
