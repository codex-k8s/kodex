import { usePlatformStore } from "@/features/platform/store";
import {
  assertOwnerRequest,
  ownerRequestSignal,
} from "@/shared/api/owner-lifetime";
import {
  assertRuntimeResourceAddressIdentity,
  requireRuntimeOrganizationRef,
  runtimeResourceAddressScope,
  type RuntimeResourceAddress,
  type RuntimeScopedResourceIdentity,
} from "./resource-scope";

export function currentRuntimeOrganizationRef(): string {
  return requireRuntimeOrganizationRef(
    usePlatformStore().bootstrap?.organizationRef,
  );
}

export function runtimeResourceOwnerBoundary(address: RuntimeResourceAddress) {
  const lifetime = ownerRequestSignal();
  assertOwnerRequest(lifetime);
  const organizationRef = currentRuntimeOrganizationRef();
  const scope = runtimeResourceAddressScope(address);
  if (
    scope.kind === "ORGANIZATION" &&
    scope.organizationRef !== organizationRef
  )
    throw new Error("Runtime resource request organization mismatch");
  function assertCurrent(): void {
    assertOwnerRequest(lifetime);
    if (currentRuntimeOrganizationRef() !== organizationRef)
      throw new Error("Runtime resource request organization changed");
  }
  function assert(resource: RuntimeScopedResourceIdentity): void {
    assertCurrent();
    assertRuntimeResourceAddressIdentity(address, resource, organizationRef);
  }
  return { organizationRef, assertCurrent, assert };
}

export function assertActiveRuntimeResourceIdentity(
  address: RuntimeResourceAddress,
  resource: RuntimeScopedResourceIdentity,
  expectedOrganizationRef?: string,
): void {
  const owner = runtimeResourceOwnerBoundary(address);
  if (
    expectedOrganizationRef &&
    owner.organizationRef !== expectedOrganizationRef
  )
    throw new Error("Runtime resource expected organization mismatch");
  owner.assert(resource);
}
