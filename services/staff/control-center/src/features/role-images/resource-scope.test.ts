import { describe, expect, it } from "vitest";
import {
  assertRoleImageResourceIdentity,
  roleImageCatalogPath,
  roleImageScopeKey,
} from "./resource-scope";

describe("Области каталога образов", () => {
  const organization = {
    kind: "ORGANIZATION",
    organizationRef: "org_alpha",
  } as const;
  it("сохраняет явный проектный адаптер без служебного проекта", () => {
    expect(roleImageCatalogPath("prj_alpha")).toBe(
      "/projects/prj_alpha/role-images",
    );
    expect(roleImageScopeKey("prj_alpha")).toBe("prj_alpha");
    expect(() => roleImageCatalogPath("")).toThrow();
    expect(roleImageCatalogPath(organization)).toBe(
      "/organization/role-images",
    );
  });
  it("не смешивает каталоги организаций в одном кэше", () => {
    expect(roleImageScopeKey(organization)).not.toBe(
      roleImageScopeKey({ ...organization, organizationRef: "org_beta" }),
    );
  });
  it("закрыто отклоняет чужую организацию, проект и неопределённую область", () => {
    const systemImage = {
      scopeKind: "ORGANIZATION",
      organizationRef: "org_alpha",
    } as const;
    expect(() =>
      assertRoleImageResourceIdentity(organization, systemImage),
    ).not.toThrow();
    for (const value of [
      { ...systemImage, organizationRef: "org_beta" },
      { ...systemImage, projectRef: "prj_alpha" },
      { ...systemImage, scopeKind: "UNSPECIFIED" as const },
    ])
      expect(() =>
        assertRoleImageResourceIdentity(organization, value),
      ).toThrow();
    expect(() =>
      assertRoleImageResourceIdentity("prj_alpha", {
        ...systemImage,
        scopeKind: "PROJECT",
        projectRef: "prj_beta",
      }),
    ).toThrow();
  });
});
