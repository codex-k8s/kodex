import {
  getRoleImageBuild,
  getRoleImageRecipe,
  manageImageBuild,
  manageRoleImageRecipe,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ManageImageBuild,
  ManageRoleImageRecipe,
  Resource,
  RoleImageRecipeReadback,
  RoleImageRecipeResult,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { etag, executeMutation } from "@/shared/lib/identity";

export async function commandRoleImage(
  body: ManageRoleImageRecipe,
  version?: number,
): Promise<RoleImageRecipeResult> {
  return (
    await executeMutation(
      `role-image:${body.action}:${body.recipeId ?? "new"}`,
      body,
      version,
      (headers) =>
        manageRoleImageRecipe({
          body,
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match"?: string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function fetchRoleImageRecipe(
  recipeId: string,
  version: number,
): Promise<RoleImageRecipeReadback> {
  return (
    await unwrap(
      getRoleImageRecipe({
        path: { recipeId },
        headers: { "If-Match": etag(version) },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function commandImageBuild(
  body: ManageImageBuild,
  version: number,
): Promise<Resource> {
  return (
    await executeMutation(
      `image-build:${body.action}:${body.imageBuildId}`,
      body,
      version,
      (headers) =>
        manageImageBuild({
          body,
          headers: headers as {
            "X-CSRF-Token": string;
            "Idempotency-Key": string;
            "If-Match": string;
          },
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function fetchRoleImageBuild(
  imageBuildId: string,
  version: number,
): Promise<Resource> {
  return (
    await unwrap(
      getRoleImageBuild({
        path: { imageBuildId },
        headers: { "If-Match": etag(version) },
        signal: requestSignal(),
      }),
    )
  ).data;
}
