import type {
  RuntimeSecret,
  RuntimeSecretPage,
} from "@/shared/api/generated/openapi/types.gen";
import type { RuntimeSecretCatalog } from "./secret-catalog";
import {
  assertRuntimeResourceIdentity,
  runtimeResourceScopeKey,
  type RuntimeResourceScope,
  type RuntimeScopedResourceIdentity,
} from "./resource-scope";

type ScopedSecret = Omit<RuntimeSecret, "scopeKind"> &
  RuntimeScopedResourceIdentity;
export interface ScopedSecretCatalogReader {
  list(
    scope: RuntimeResourceScope,
    query: string,
    cursor: string | undefined,
    signal: AbortSignal,
    pageSize: number,
  ): Promise<Omit<RuntimeSecretPage, "items"> & { items: ScopedSecret[] }>;
  read(
    scope: RuntimeResourceScope,
    ref: string,
    signal: AbortSignal,
  ): Promise<ScopedSecret>;
}

export function createScopedRuntimeSecretCatalog(
  organizationRef: string,
  reader: ScopedSecretCatalogReader,
): RuntimeSecretCatalog {
  if (!/^[A-Za-z0-9_-]{8,128}$/.test(organizationRef))
    throw new Error("Runtime secret catalog organization is invalid");

  function assertScope(scope: RuntimeResourceScope): void {
    runtimeResourceScopeKey(scope);
    if (
      scope.kind === "ORGANIZATION" &&
      scope.organizationRef !== organizationRef
    )
      throw new Error("Runtime secret catalog organization scope mismatch");
  }

  function metadata(
    scope: RuntimeResourceScope,
    secret: ScopedSecret,
  ): RuntimeSecret {
    assertRuntimeResourceIdentity(scope, secret, organizationRef);
    if (secret.scopeKind === "UNSPECIFIED")
      throw new Error("Runtime secret catalog scope is invalid");
    if (
      !secret.ref ||
      !Number.isSafeInteger(secret.version) ||
      secret.version < 1 ||
      !Number.isSafeInteger(secret.currentRevision) ||
      secret.currentRevision < 1 ||
      !["ACTIVE", "REVOKED"].includes(secret.state) ||
      !["STRING", "JSON", "BINARY"].includes(secret.valueType) ||
      typeof secret.name !== "string" ||
      typeof secret.description !== "string"
    )
      throw new Error("Runtime secret catalog metadata is invalid");
    // Каталог для привязки не получает и не сохраняет материал секрета.
    return {
      ref: secret.ref,
      version: secret.version,
      scopeKind: secret.scopeKind,
      organizationRef: secret.organizationRef,
      projectRef: secret.projectRef,
      name: secret.name,
      description: secret.description,
      valueType: secret.valueType,
      state: secret.state,
      currentRevision: secret.currentRevision,
      displayHint: secret.displayHint
        ? {
            prefix: secret.displayHint.prefix,
            suffix: secret.displayHint.suffix,
          }
        : undefined,
      nextActions: [...secret.nextActions],
      createdAt: secret.createdAt,
      updatedAt: secret.updatedAt,
    };
  }

  return {
    async loadPage(scope, query, cursor, signal, pageSize = 30) {
      assertScope(scope);
      const page = await reader.list(scope, query, cursor, signal, pageSize);
      signal.throwIfAborted();
      if (
        !Array.isArray(page.items) ||
        (page.nextPageToken && page.nextPageToken === cursor) ||
        new Set(page.items.map((secret) => secret.ref)).size !==
          page.items.length
      )
        throw new Error("Runtime secret catalog page is invalid");
      return {
        items: page.items.map((secret) => metadata(scope, secret)),
        nextPageToken: page.nextPageToken,
      };
    },
    async read(scope, ref, signal) {
      assertScope(scope);
      const secret = await reader.read(scope, ref, signal);
      signal.throwIfAborted();
      if (secret.ref !== ref)
        throw new Error("Runtime secret catalog reference mismatch");
      return metadata(scope, secret);
    },
  };
}
