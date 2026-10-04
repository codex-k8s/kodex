import {
  assertRuntimeResourceAddressIdentity,
  runtimeResourceAddressKey,
  runtimeResourceAddressScope,
  runtimeResourceCatalogPath,
  type RuntimeResourceScope,
  type RuntimeResourceAddress,
  type RuntimeScopedResourceIdentity,
} from "@/features/runtime/resource-scope";

export type RoleImageResourceScope = RuntimeResourceAddress;

export function roleImageRuntimeScope(
  scope: RoleImageResourceScope,
): RuntimeResourceScope {
  return runtimeResourceAddressScope(scope);
}

export function roleImageScopeKey(scope: RoleImageResourceScope): string {
  return runtimeResourceAddressKey(scope);
}

export function roleImageCatalogPath(scope: RoleImageResourceScope): string {
  return runtimeResourceCatalogPath(
    roleImageRuntimeScope(scope),
    "role-images",
  );
}

export function assertRoleImageResourceIdentity(
  scope: RoleImageResourceScope,
  resource: RuntimeScopedResourceIdentity,
  organizationRef?: string,
): void {
  assertRuntimeResourceAddressIdentity(scope, resource, organizationRef);
}
