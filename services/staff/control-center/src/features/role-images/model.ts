import type {
  RoleImageArtifact,
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

const buildStageOrder: Record<RoleImageBuild["stage"], number> = {
  QUEUED: 0,
  MATERIALIZATION: 1,
  CONTEXT_VALIDATION: 2,
  BASE_PULL: 3,
  SOLVING: 4,
  INSTALLATION: 5,
  TRUSTED_RUNTIME_FINALIZATION: 6,
  STAGING_PUSH: 7,
  PROVENANCE: 8,
  COMPLETED: 9,
  FAILED: 9,
  CANCELLED: 9,
  EXPIRED: 9,
  DEAD_LETTER: 9,
};

export function latestBuild(
  builds: readonly RoleImageBuild[],
): RoleImageBuild | undefined {
  return [...builds].sort((left, right) => {
    const time = Date.parse(right.updatedAt) - Date.parse(left.updatedAt);
    if (time !== 0) return time;
    if (right.attempt !== left.attempt) return right.attempt - left.attempt;
    return buildStageOrder[right.stage] - buildStageOrder[left.stage];
  })[0];
}

export function buildIsTerminal(build: RoleImageBuild): boolean {
  return [
    "COMPLETED",
    "FAILED",
    "CANCELLED",
    "EXPIRED",
    "DEAD_LETTER",
  ].includes(build.stage);
}

export function buildIsActive(build: RoleImageBuild): boolean {
  return !buildIsTerminal(build);
}

export function canRequestBuild(recipe: RoleImageRecipe): boolean {
  return (
    recipe.state === "ACTIVE" && recipe.nextActions.includes("REQUEST_BUILD")
  );
}

export function canPromoteRoleImage(
  recipe: RoleImageRecipe,
  artifact?: RoleImageArtifact,
): boolean {
  return Boolean(
    recipe.nextActions.includes("PROMOTE") &&
    artifact?.admissionVerdict === "ACCEPTED" &&
    artifact.provenanceSha256,
  );
}

export function buildRevisionIdentity(build: RoleImageBuild): {
  generation: number;
  attempt: number;
} {
  return {
    generation: build.recipeGeneration,
    attempt: build.attempt,
  };
}

export function roleImageState(
  recipe: RoleImageRecipe,
  build?: RoleImageBuild,
): RoleImageBuild["stage"] | RoleImageRecipe["state"] | "PROMOTED" {
  if (recipe.state === "ARCHIVED") return "ARCHIVED";
  if (build && build.stage !== "COMPLETED") return build.stage;
  if (recipe.promotedImageReady) return "PROMOTED";
  return build?.stage ?? recipe.state;
}

export function validateDockerfile(value: string): string[] {
  const normalized = value.replace(/\r\n?/g, "\n").trim();
  if (!normalized) return ["roleImages.validation.dockerfileRequired"];
  const meaningful = normalized
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith("#"));
  if (!meaningful.some((line) => /^FROM\s+/i.test(line)))
    return ["roleImages.validation.fromRequired"];
  return [];
}
