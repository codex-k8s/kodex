export type RuntimeResourceScope =
  | { kind: "ORGANIZATION"; readonly organizationRef: string }
  | { kind: "PROJECT"; projectRef: string };

export type RuntimeResourceAddress =
  | string
  | Extract<RuntimeResourceScope, { kind: "ORGANIZATION" }>;

export function parseRuntimeResourceScope(
  value: unknown,
): RuntimeResourceScope {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    !("kind" in value)
  )
    throw new Error("Runtime resource scope shape is invalid");
  if (
    value.kind === "ORGANIZATION" &&
    "organizationRef" in value &&
    Object.keys(value).sort().join(",") === "kind,organizationRef" &&
    typeof value.organizationRef === "string"
  ) {
    const scope = {
      kind: "ORGANIZATION" as const,
      organizationRef: value.organizationRef,
    };
    runtimeResourceScopeKey(scope);
    return scope;
  }
  if (
    value.kind === "PROJECT" &&
    "projectRef" in value &&
    Object.keys(value).sort().join(",") === "kind,projectRef" &&
    typeof value.projectRef === "string"
  ) {
    const scope = { kind: "PROJECT" as const, projectRef: value.projectRef };
    runtimeResourceScopeKey(scope);
    return scope;
  }
  throw new Error("Runtime resource scope is invalid");
}

export function runtimeResourceAddressScope(
  address: RuntimeResourceAddress,
): RuntimeResourceScope {
  const scope =
    typeof address === "string"
      ? { kind: "PROJECT" as const, projectRef: address }
      : parseRuntimeResourceScope(address);
  runtimeResourceScopeKey(scope);
  return scope;
}

export function runtimeResourceAddressKey(
  address: RuntimeResourceAddress,
): string {
  const scope = runtimeResourceAddressScope(address);
  return scope.kind === "PROJECT"
    ? scope.projectRef
    : runtimeResourceScopeKey(scope);
}

export function assertRuntimeResourceAddressIdentity(
  address: RuntimeResourceAddress,
  resource: RuntimeScopedResourceIdentity,
  organizationRef?: string,
): void {
  const scope = runtimeResourceAddressScope(address);
  assertRuntimeResourceIdentity(
    scope,
    resource,
    organizationRef ??
      (scope.kind === "ORGANIZATION"
        ? scope.organizationRef
        : resource.organizationRef),
  );
}

export interface RuntimeScopedResourceIdentity {
  readonly scopeKind: "ORGANIZATION" | "PROJECT" | "UNSPECIFIED";
  readonly organizationRef: string;
  readonly projectRef?: string;
}

export function runtimeResourceAddressFromIdentity(
  resource: RuntimeScopedResourceIdentity,
): RuntimeResourceAddress {
  const address: RuntimeResourceAddress =
    resource.scopeKind === "ORGANIZATION"
      ? { kind: "ORGANIZATION", organizationRef: resource.organizationRef }
      : resource.scopeKind === "PROJECT" &&
          typeof resource.projectRef === "string"
        ? resource.projectRef
        : "";
  assertRuntimeResourceAddressIdentity(address, resource);
  return address;
}

export function organizationRuntimeResourceScope(
  bootstrap: unknown,
): Extract<RuntimeResourceScope, { kind: "ORGANIZATION" }> | undefined {
  if (
    !bootstrap ||
    typeof bootstrap !== "object" ||
    !("organizationRef" in bootstrap) ||
    typeof bootstrap.organizationRef !== "string" ||
    !/^[A-Za-z0-9_-]{8,128}$/.test(bootstrap.organizationRef)
  )
    return undefined;
  return { kind: "ORGANIZATION", organizationRef: bootstrap.organizationRef };
}

export function assertRuntimeResourceIdentity(
  scope: RuntimeResourceScope,
  resource: RuntimeScopedResourceIdentity,
  organizationRef: string,
): void {
  runtimeResourceScopeKey(scope);
  if (
    !/^[A-Za-z0-9_-]{8,128}$/.test(organizationRef) ||
    resource.organizationRef !== organizationRef ||
    (scope.kind === "ORGANIZATION" &&
      scope.organizationRef !== organizationRef) ||
    resource.scopeKind !== scope.kind ||
    (scope.kind === "ORGANIZATION"
      ? resource.projectRef !== undefined && resource.projectRef !== ""
      : resource.projectRef !== scope.projectRef)
  )
    throw new Error("Runtime resource owner scope mismatch");
}

export function runtimeResourceCatalogPath(
  scope: RuntimeResourceScope,
  kind: "role-images" | "secrets" | "environments",
): string {
  runtimeResourceScopeKey(scope);
  return scope.kind === "ORGANIZATION"
    ? `/organization/${kind}`
    : `/projects/${encodeURIComponent(scope.projectRef)}/${kind}`;
}

export function runtimeResourceScopeKey(scope: RuntimeResourceScope): string {
  if (scope.kind === "ORGANIZATION") {
    if (!/^[A-Za-z0-9_-]{8,128}$/.test(scope.organizationRef))
      throw new Error("Runtime resource organization scope is invalid");
    return `ORGANIZATION:${scope.organizationRef}`;
  }
  if (!/^[A-Za-z0-9_-]{8,128}$/.test(scope.projectRef))
    throw new Error("Runtime resource project scope is invalid");
  return `PROJECT:${scope.projectRef}`;
}

export function assertRuntimeResourceScope(
  scope: RuntimeResourceScope,
  projectRef: string | undefined,
): void {
  runtimeResourceScopeKey(scope);
  if (
    scope.kind === "ORGANIZATION"
      ? Boolean(projectRef)
      : projectRef !== scope.projectRef
  )
    throw new Error("Runtime resource scope mismatch");
}
