import {
  loadRuntimeSecretPage,
  readRuntimeSecret,
} from "@/features/runtime-secrets/api";
import {
  loadRoleImageDetail,
  loadRoleImagePage,
} from "@/features/role-images/api";
import { createScopedRuntimeImageCatalog } from "./scoped-image-catalog";
import { createScopedRuntimeSecretCatalog } from "./scoped-secret-catalog";

export function createRuntimeResourceCatalogs(organizationRef: string) {
  const images = createScopedRuntimeImageCatalog(organizationRef, {
    list: (scope, cursor, signal, pageSize) =>
      loadRoleImagePage(
        scope.kind === "PROJECT" ? scope.projectRef : scope,
        cursor,
        signal,
        {},
        pageSize,
      ),
    read: (scope, recipeRef, signal) =>
      loadRoleImageDetail(
        scope.kind === "PROJECT" ? scope.projectRef : scope,
        recipeRef,
        signal,
      ),
  });
  const secrets = createScopedRuntimeSecretCatalog(organizationRef, {
    async list(scope, search, cursor, signal, pageSize) {
      return loadRuntimeSecretPage(
        scope.kind === "PROJECT" ? scope.projectRef : scope,
        search,
        cursor,
        signal,
        pageSize,
      );
    },
    async read(scope, secretRef, signal) {
      return readRuntimeSecret(
        secretRef,
        scope.kind === "PROJECT" ? scope.projectRef : scope,
        signal,
      );
    },
  });
  return { images, secrets };
}
