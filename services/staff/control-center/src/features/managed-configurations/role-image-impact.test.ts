import { expect, it } from "vitest";
import type {
  RoleImageImpactPlan,
  RoleImageImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import { checkedImagePage } from "./role-image-impact";
const plan: RoleImageImpactPlan = {
  ref: "plan",
  version: 1,
  configurationRef: "configuration",
  configurationVersion: 2,
  revisionRef: "revision",
  revisionDigest: "revision-digest",
  recipeRef: "recipe",
  recipeGeneration: 3,
  buildRef: "build",
  artifactRef: "artifact",
  artifactDigest: "artifact-digest",
  admissionPolicyDigest: "admission",
  digest: "plan-digest",
  total: 3,
  state: "PREPARED",
  createdAt: "2026-09-06T00:00:00Z",
  expiresAt: "2099-09-06T00:00:00Z",
};
const page: RoleImageImpactPage = {
  plan,
  total: 1,
  items: [
    {
      ref: "item",
      environmentRef: "environment",
      environmentVersion: 4,
      sourceVersionRef: "source",
      sourceVersionDigest: "source-digest",
      projectRef: "project_synthetic",
      scopeKind: "PROJECT",
      organizationRef: "org_synthetic",
      outcome: "PENDING",
    },
  ],
  nextPageToken: "next",
};
it("system image impact отличает явный ORG от пустого неподтверждённого project", () => {
  const original = page.items[0];
  if (!original) throw new Error("Synthetic impact item is missing");
  const owned = { ...original, scopeKind: "ORGANIZATION" as const, projectRef: "" };
  expect(checkedImagePage({ ...page, items: [owned] }, plan, undefined, "org_synthetic").items).toEqual([owned]);
  expect(() => checkedImagePage({ ...page, items: [{ ...owned, scopeKind: "PROJECT" }] }, plan, undefined, "org_synthetic")).toThrow();
  expect(() => checkedImagePage({ ...page, items: [{ ...owned, organizationRef: "org_foreign" }] }, plan, undefined, "org_synthetic")).toThrow();
});
it("сохраняет Environment без Agent и независимый owner count", () => {
  const result = checkedImagePage(page, plan, undefined, "org_synthetic");
  expect(result.plan.total).toBe(3);
  expect(result.total).toBe(1);
  expect(result.items[0]?.consumer).toBeUndefined();
});
it("отклоняет смену admission/build/source и повтор cursor", () => {
  for (const changed of [
    { artifactDigest: "other" },
    { buildRef: "other" },
    { admissionPolicyDigest: "other" },
    { configurationVersion: 3 },
  ])
    expect(() =>
      checkedImagePage(
        { ...page, plan: { ...plan, ...changed } },
        plan,
        undefined,
        "org_synthetic",
      ),
    ).toThrow();
  expect(() => checkedImagePage(page, plan, "next", "org_synthetic")).toThrow();
  expect(() =>
    checkedImagePage({ ...page, total: 4 }, plan, undefined, "org_synthetic"),
  ).toThrow();
});
it("восстанавливает APPLIED план с отдельным конфликтом, сбрасывает старый cursor", () => {
  const applied: RoleImageImpactPage = {
    ...page,
    plan: { ...plan, version: 2, state: "APPLIED" },
    items: page.items.map((item) => ({ ...item, outcome: "CONFLICT" })),
  };
  expect(
    checkedImagePage(applied, plan, undefined, "org_synthetic").items[0]
      ?.outcome,
  ).toBe("CONFLICT");
  expect(() =>
    checkedImagePage(applied, plan, "old", "org_synthetic"),
  ).toThrow();
});
it("проверяет фактическую новую Environment revision для APPLIED", () => {
  const applied: RoleImageImpactPage = {
    ...page,
    plan: { ...plan, version: 2, state: "APPLIED" },
    items: page.items.map((item) => ({
      ...item,
      outcome: "APPLIED",
      resultEnvironmentVersionRef: "new-version",
    })),
  };
  expect(
    checkedImagePage(applied, plan, undefined, "org_synthetic").items[0]
      ?.resultEnvironmentVersionRef,
  ).toBe("new-version");
  expect(() =>
    checkedImagePage(
      {
        ...applied,
        items: applied.items.map((item) => ({
          ...item,
          resultEnvironmentVersionRef: undefined,
        })),
      },
      plan,
      undefined,
      "org_synthetic",
    ),
  ).toThrow();
});
