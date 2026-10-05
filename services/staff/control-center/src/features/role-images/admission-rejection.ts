import type {
  RoleImageArtifact,
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";

export function currentRoleImageAdmissionRejected(
  recipe?: RoleImageRecipe,
  build?: RoleImageBuild,
  artifact?: RoleImageArtifact,
): boolean {
  return Boolean(
    recipe &&
    build &&
    artifact &&
    ["ORGANIZATION", "PROJECT"].includes(recipe.scopeKind) &&
    recipe.organizationRef &&
    (recipe.scopeKind === "ORGANIZATION"
      ? recipe.projectRef === ""
      : recipe.projectRef) &&
    build.stage === "COMPLETED" &&
    build.recipeRef === recipe.ref &&
    build.recipeGeneration === recipe.generation &&
    artifact.admissionVerdict === "REJECTED" &&
    artifact.ref &&
    Number.isSafeInteger(artifact.version) &&
    artifact.version > 0 &&
    artifact.recipeRef === recipe.ref &&
    artifact.buildRef === build.ref &&
    artifact.recipeGeneration === recipe.generation &&
    [build, artifact].every(
      (value) =>
        value.scopeKind === recipe.scopeKind &&
        value.organizationRef === recipe.organizationRef &&
        value.projectRef === recipe.projectRef,
    ),
  );
}
