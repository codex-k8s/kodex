import type {
  RoleImageAdmissionFailure,
  RoleImageBuild,
  RoleImageRecipe,
  RoleImageRecipeDetail,
} from "@/shared/api/generated/openapi/types.gen";
import { latestBuild } from "./model";

export function currentRoleImageAdmissionFailure(
  recipe: RoleImageRecipe | undefined,
  build: RoleImageBuild | undefined,
  failure: RoleImageAdmissionFailure | undefined,
): RoleImageAdmissionFailure | undefined {
  if (
    !recipe ||
    !build ||
    !failure ||
    build.stage !== "COMPLETED" ||
    build.recipeRef !== recipe.ref ||
    build.recipeGeneration !== recipe.generation ||
    !["FAILED"].includes(failure.state) ||
    !failure.imageArtifactRef ||
    !Number.isSafeInteger(failure.version) ||
    failure.version < 1 ||
    failure.recipeRef !== recipe.ref ||
    failure.recipeGeneration !== recipe.generation ||
    failure.buildRef !== build.ref ||
    failure.buildAttempt !== build.attempt ||
    ![
      "ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND",
      "ADMISSION_EVIDENCE_EXCEEDS_BOUND",
      "ADMISSION_WORKER_FAILED",
      "ADMISSION_LEASE_EXPIRED",
    ].includes(failure.errorCode) ||
    !["PROJECT", "ORGANIZATION"].includes(recipe.scopeKind) ||
    !recipe.organizationRef ||
    (recipe.scopeKind === "ORGANIZATION"
      ? recipe.projectRef !== ""
      : !recipe.projectRef) ||
    [build, failure].some(
      (value) =>
        value.scopeKind !== recipe.scopeKind ||
        value.organizationRef !== recipe.organizationRef ||
        value.projectRef !== recipe.projectRef,
    )
  )
    return undefined;
  return failure;
}

export function assertRoleImageAdmissionFailure(
  detail: RoleImageRecipeDetail,
): void {
  if (!detail.admissionFailure) return;
  const failure = currentRoleImageAdmissionFailure(
    detail.recipe,
    latestBuild(detail.builds),
    detail.admissionFailure,
  );
  if (
    !failure ||
    detail.promotionCandidate?.buildRef === failure.buildRef ||
    detail.activeArtifact?.buildRef === failure.buildRef
  )
    throw new Error("Invalid role image admission failure identity");
}
