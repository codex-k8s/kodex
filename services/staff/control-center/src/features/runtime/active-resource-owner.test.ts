import { beforeEach, expect, it } from "vitest";
import { initializeRuntimeOwnerFixture } from "@/test-utils/runtime-owner-fixture";
import { usePlatformStore } from "@/features/platform/store";
import type { BootstrapState } from "@/shared/api/generated/openapi/types.gen";
import { resetOwnerRequests } from "@/shared/api/owner-lifetime";
import {
  assertRuntimeResourceAddressIdentity,
  runtimeResourceAddressFromIdentity,
} from "./resource-scope";
import {
  assertActiveRuntimeResourceIdentity,
  runtimeResourceOwnerBoundary,
} from "./active-resource-owner";

const project = {
  scopeKind: "PROJECT",
  organizationRef: "org_synthetic",
  projectRef: "project_synthetic",
} as const;
const organization = {
  scopeKind: "ORGANIZATION",
  organizationRef: "org_synthetic",
  projectRef: "",
} as const;
beforeEach(() => initializeRuntimeOwnerFixture("org_synthetic"));

it("адрес из DTO проверяет только форму, но assert identity требует внешний anchor", () => {
  expect(runtimeResourceAddressFromIdentity(project)).toBe(project.projectRef);
  expect(runtimeResourceAddressFromIdentity(organization)).toEqual({
    kind: "ORGANIZATION",
    organizationRef: organization.organizationRef,
  });
  expect(() =>
    assertRuntimeResourceAddressIdentity(project.projectRef, project),
  ).toThrow("anchor");
  expect(() =>
    assertRuntimeResourceAddressIdentity(
      { kind: "ORGANIZATION", organizationRef: organization.organizationRef },
      organization,
    ),
  ).toThrow("anchor");
});
it.each([project, organization])(
  "отклоняет missing/foreign bootstrap для $scopeKind без DTO authority",
  (resource) => {
    const address = runtimeResourceAddressFromIdentity(resource);
    expect(() =>
      assertActiveRuntimeResourceIdentity(address, resource),
    ).not.toThrow();
    usePlatformStore().bootstrap = undefined;
    expect(() =>
      assertActiveRuntimeResourceIdentity(address, resource),
    ).toThrow("anchor");
    usePlatformStore().bootstrap = {
      organizationRef: "org_foreign",
    } as BootstrapState;
    expect(() =>
      assertActiveRuntimeResourceIdentity(address, resource),
    ).toThrow();
  },
);
it("прежний запрос закрыто отклоняет поздний ответ даже после совпадения нового DTO с новым owner", () => {
  const request = runtimeResourceOwnerBoundary(project.projectRef);
  usePlatformStore().bootstrap = {
    organizationRef: "org_foreign",
  } as BootstrapState;
  expect(() =>
    request.assert({ ...project, organizationRef: "org_foreign" }),
  ).toThrow("changed");
});
it("смена session lifetime отклоняет прежний ответ даже в той же организации", () => {
  const request = runtimeResourceOwnerBoundary(project.projectRef);
  resetOwnerRequests();
  expect(() => request.assert(project)).toThrow();
});
