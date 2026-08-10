import type {
  Resource,
  RoleImageRecipeInput,
  RoleImageRecipeReadback,
} from "@/shared/api/generated/openapi/types.gen";

export interface RoleImagePackageModel {
  manager: "apk" | "apt" | "dnf" | "pip" | "npm";
  name: string;
  version: string;
  digest: string;
  sourceRef: string;
}

export interface RoleImageToolModel {
  name: string;
  version: string;
  sourceRef: string;
  sha256: string;
}

export interface RoleImageInputModel {
  baseImageReference: string;
  baseImageDigest: string;
  sourceRef: string;
  sourceRevision: string;
  sourceSha256: string;
  contextRef: string;
  contextSha256: string;
  builderSha256: string;
  frontendSha256: string;
  platforms: Array<{ os: "linux"; architecture: "amd64" | "arm64" }>;
  packages: RoleImagePackageModel[];
  tools: RoleImageToolModel[];
  installationBlock: string;
  toolchainSha256: string;
}

export interface RoleImageResourceModel {
  id: string;
  kind: string;
  name: string;
  state: string;
  version: number;
  recipeBaseImageReference: string;
  buildStage: string;
  buildProgressPercent: number;
  buildSpecSha256: string;
}

export interface RoleImageRecipeDetailModel {
  recipeName: string;
  input: RoleImageInputModel;
}

export const emptyRoleImageInput = (): RoleImageInputModel => ({
  baseImageReference: "",
  baseImageDigest: "",
  sourceRef: "",
  sourceRevision: "",
  sourceSha256: "",
  contextRef: "",
  contextSha256: "",
  builderSha256: "",
  frontendSha256: "",
  platforms: [{ os: "linux", architecture: "amd64" }],
  packages: [],
  tools: [],
  installationBlock: "",
  toolchainSha256: "",
});

const toInputModel = (value: RoleImageRecipeInput): RoleImageInputModel => ({
  baseImageReference: value.baseImageReference,
  baseImageDigest: value.baseImageDigest,
  sourceRef: value.sourceRef,
  sourceRevision: value.sourceRevision,
  sourceSha256: value.sourceSha256,
  contextRef: value.contextRef,
  contextSha256: value.contextSha256,
  builderSha256: value.builderSha256,
  frontendSha256: value.frontendSha256,
  platforms: value.platforms.map((item) => ({
    os: item.os,
    architecture: item.architecture,
  })),
  packages: value.packages.map((item) => ({ ...item })),
  tools: value.tools.map((item) => ({ ...item })),
  installationBlock: value.installationBlock,
  toolchainSha256: value.toolchainSha256,
});

export const toRoleImageInput = (
  value: RoleImageInputModel,
): RoleImageRecipeInput => ({
  baseImageReference: value.baseImageReference,
  baseImageDigest: value.baseImageDigest,
  sourceRef: value.sourceRef,
  sourceRevision: value.sourceRevision,
  sourceSha256: value.sourceSha256,
  contextRef: value.contextRef,
  contextSha256: value.contextSha256,
  builderSha256: value.builderSha256,
  frontendSha256: value.frontendSha256,
  platforms: value.platforms.map((item) => ({ ...item })),
  packages: value.packages.map((item) => ({ ...item })),
  tools: value.tools.map((item) => ({ ...item })),
  installationBlock: value.installationBlock,
  toolchainSha256: value.toolchainSha256,
});

export const toRoleImageResourceModel = (
  value: Resource,
): RoleImageResourceModel => ({
  id: value.id,
  kind: value.kind,
  name: value.name,
  state: value.state,
  version: value.version,
  recipeBaseImageReference:
    value.spec.roleImageRecipe?.input.baseImageReference ?? "",
  buildStage: value.spec.imageBuild?.stage ?? value.state,
  buildProgressPercent: value.spec.imageBuild?.progressPercent ?? 0,
  buildSpecSha256: value.spec.imageBuild?.specSha256 ?? "",
});

export const toRoleImageRecipeDetailModel = (
  value: RoleImageRecipeReadback,
): RoleImageRecipeDetailModel => ({
  recipeName: value.recipe.name,
  input: toInputModel(value.input),
});
