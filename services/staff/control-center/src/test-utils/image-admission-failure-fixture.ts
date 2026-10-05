import type {
  RoleImageAdmissionFailure,
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

export function imageAdmissionFailureFixture(
  recipe: RoleImageRecipe,
  build: RoleImageBuild,
  overrides: Partial<RoleImageAdmissionFailure> = {},
): RoleImageAdmissionFailure {
  return {
    scopeKind: recipe.scopeKind,
    organizationRef: recipe.organizationRef,
    projectRef: recipe.projectRef,
    imageArtifactRef: "imgart_failed_synthetic",
    version: 2,
    recipeRef: recipe.ref,
    recipeGeneration: recipe.generation,
    buildRef: build.ref,
    buildAttempt: build.attempt,
    state: "FAILED",
    errorCode: "ADMISSION_WORKER_FAILED",
    ...overrides,
  };
}
