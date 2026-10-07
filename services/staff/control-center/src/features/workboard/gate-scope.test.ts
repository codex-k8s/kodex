import { describe, expect, it } from "vitest";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { assertGateScope, gateScopeKey, hasValidGateScope } from "./gate-scope";

const project = {
  scopeKind: "PROJECT",
  organizationRef: "org_test",
  projectRef: "project_test",
} as OwnerGate;
const organization = {
  scopeKind: "ORGANIZATION",
  organizationRef: "org_test",
} as OwnerGate;

describe("явная область решения владельца", () => {
  it("различает проект и организацию и сверяет exact owner boundary", () => {
    expect(hasValidGateScope(project, "org_test")).toBe(true);
    expect(hasValidGateScope(organization, "org_test")).toBe(true);
    expect(gateScopeKey(project)).not.toBe(gateScopeKey(organization));
    expect(hasValidGateScope(organization, "org_other")).toBe(false);
  });
  it.each([
    { ...organization, scopeKind: undefined },
    { ...organization, scopeKind: "UNKNOWN" },
    { ...organization, organizationRef: "" },
    { ...organization, projectRef: "project_test" },
    { ...organization, projectRef: "" },
    { ...project, projectRef: undefined },
    { ...project, projectRef: " " },
  ])("не угадывает область по пустому projectRef", (gate) => {
    expect(hasValidGateScope(gate as OwnerGate)).toBe(false);
    expect(() => assertGateScope(gate as OwnerGate)).toThrow(
      "Invalid owner gate scope",
    );
  });
});
