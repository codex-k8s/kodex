import { describe, expect, it } from "vitest";
import {
  assertRuntimeResourceScope,
  assertRuntimeResourceIdentity,
  runtimeResourceCatalogPath,
  runtimeResourceScopeKey,
  organizationRuntimeResourceScope,
} from "./resource-scope";

describe("Явная область runtime-ресурса", () => {
  it("получает область организации только из проверенного bootstrap", () => {
    expect(
      organizationRuntimeResourceScope({ organizationRef: "org_alpha" }),
    ).toEqual({ kind: "ORGANIZATION", organizationRef: "org_alpha" });
    for (const value of [
      undefined,
      {},
      { projectRef: "prj_alpha" },
      { organizationRef: "" },
    ])
      expect(organizationRuntimeResourceScope(value)).toBeUndefined();
  });
  it("строит общесистемные маршруты отдельно от совокупности проектов", () => {
    expect(
      runtimeResourceCatalogPath(
        { kind: "ORGANIZATION", organizationRef: "org_alpha" },
        "secrets",
      ),
    ).toBe("/organization/secrets");
    expect(
      runtimeResourceCatalogPath(
        { kind: "PROJECT", projectRef: "prj_alpha" },
        "role-images",
      ),
    ).toBe("/projects/prj_alpha/role-images");
  });
  it("проверяет scopeKind и организацию, а не угадывает область по пустому проекту", () => {
    const scope = {
      kind: "ORGANIZATION",
      organizationRef: "org_alpha",
    } as const;
    const resource = {
      scopeKind: "ORGANIZATION",
      organizationRef: "org_alpha",
    } as const;
    expect(() =>
      assertRuntimeResourceIdentity(scope, resource, "org_alpha"),
    ).not.toThrow();
    for (const invalid of [
      { ...resource, scopeKind: "UNSPECIFIED" as const },
      { ...resource, scopeKind: "PROJECT" as const },
      { ...resource, organizationRef: "org_beta" },
      { ...resource, projectRef: "prj_alpha" },
    ])
      expect(() =>
        assertRuntimeResourceIdentity(scope, invalid, "org_alpha"),
      ).toThrow();
    expect(() =>
      assertRuntimeResourceIdentity(
        { kind: "PROJECT", projectRef: "prj_alpha" },
        { ...resource, scopeKind: "PROJECT", projectRef: "prj_beta" },
        "org_alpha",
      ),
    ).toThrow();
  });
  it("различает организацию и проекты без служебного projectRef", () => {
    expect(
      runtimeResourceScopeKey({
        kind: "ORGANIZATION",
        organizationRef: "org_alpha",
      }),
    ).toBe("ORGANIZATION:org_alpha");
    expect(
      runtimeResourceScopeKey({ kind: "PROJECT", projectRef: "prj_alpha" }),
    ).toBe("PROJECT:prj_alpha");
    expect(() =>
      runtimeResourceScopeKey({ kind: "PROJECT", projectRef: "" }),
    ).toThrow();
  });
  it("не выдаёт проектный ресурс за системный", () => {
    expect(() =>
      assertRuntimeResourceScope(
        { kind: "ORGANIZATION", organizationRef: "org_alpha" },
        "prj_alpha",
      ),
    ).toThrow();
    expect(() =>
      assertRuntimeResourceScope(
        { kind: "PROJECT", projectRef: "prj_alpha" },
        undefined,
      ),
    ).toThrow();
    expect(() =>
      assertRuntimeResourceScope(
        { kind: "PROJECT", projectRef: "prj_alpha" },
        "prj_beta",
      ),
    ).toThrow();
  });
});
