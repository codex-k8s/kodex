import { requestSignal } from "@/shared/api/client";
import {
  getRuntimeSecret,
  listRuntimeSecrets,
  listSystemRuntimeSecrets,
} from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap } from "@/shared/api/problem";
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
      const query = {
        pageSize,
        ...(search.trim() ? { query: search.trim() } : {}),
        ...(cursor ? { pageToken: cursor } : {}),
      };
      if (scope.kind === "ORGANIZATION")
        return (
          await unwrap(
            listSystemRuntimeSecrets({ query, signal: requestSignal(signal) }),
          )
        ).data;
      return (
        await unwrap(
          listRuntimeSecrets({
            path: { projectRef: scope.projectRef },
            query,
            signal: requestSignal(signal),
          }),
        )
      ).data;
    },
    async read(_scope, secretRef, signal) {
      return (
        await unwrap(
          getRuntimeSecret({
            path: { secretRef },
            signal: requestSignal(signal),
            cache: "no-store",
          }),
        )
      ).data;
    },
  });
  return { images, secrets };
}
