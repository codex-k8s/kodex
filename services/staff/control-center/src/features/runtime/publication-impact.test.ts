import { expect, it } from "vitest";
import type {
  RevisionImpactPlan,
  RevisionImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import {
  checkedPublicationPage,
  publicationSelection,
} from "./publication-impact";

const plan: RevisionImpactPlan = {
  ref: "plan",
  version: 1,
  kind: "RUNTIME_ENVIRONMENT",
  sourceRef: "environment",
  sourceVersion: 3,
  sourceRevisionRef: "base",
  draftRef: "draft",
  draftVersion: 5,
  targetDigest: "target",
  digest: "digest",
  total: 4,
  state: "PREPARED",
  createdAt: "2026-09-06T00:00:00Z",
  expiresAt: "2099-09-06T00:00:00Z",
};
const page: RevisionImpactPage = {
  plan,
  total: 1,
  nextPageToken: "next",
  items: [
    {
      ref: "item",
      projectRef: "project",
      consumerKind: "AGENT",
      consumerRef: "agent",
      consumerVersion: 2,
      bindingRef: "binding",
      bindingVersion: 3,
      sourceRevisionRef: "base",
      outcome: "PENDING",
    },
  ],
};
it("сохраняет immutable total и текущий owner count, допускает явную публикацию без замен", () => {
  expect(checkedPublicationPage(page, plan).total).toBe(1);
  expect(checkedPublicationPage(page, plan).plan.total).toBe(4);
  expect(publicationSelection(plan, [])).toEqual({
    planRef: "plan",
    selectedItemRefs: [],
  });
});
it("отклоняет подмену snapshot, повтор cursor и рост count за immutable план", () => {
  for (const invalid of [
    { ...page, plan: { ...plan, draftVersion: 6 } },
    { ...page, plan: { ...plan, sourceRevisionRef: "other" } },
    { ...page, total: 5 },
    { ...page, items: [...page.items, ...page.items], total: 2 },
  ])
    expect(() => checkedPublicationPage(invalid, plan)).toThrow();
  expect(() => checkedPublicationPage(page, plan, "next")).toThrow();
});
it("terminal recovery начинает новую страницу и сохраняет per-item CONFLICT", () => {
  const recovered: RevisionImpactPage = {
    ...page,
    plan: {
      ...plan,
      version: 2,
      state: "APPLIED",
      publishedRevisionRef: "published",
    },
    items: page.items.map((item) => ({ ...item, outcome: "CONFLICT" })),
  };
  expect(checkedPublicationPage(recovered, plan).items[0]?.outcome).toBe(
    "CONFLICT",
  );
  expect(() => checkedPublicationPage(recovered, plan, "old-cursor")).toThrow();
});
it("не принимает APPLIED строку с чужим binding или revision", () => {
  const applied = {
    ...plan,
    version: 2,
    state: "APPLIED" as const,
    publishedRevisionRef: "published",
  };
  const result: RevisionImpactPage = {
    ...page,
    plan: applied,
    items: page.items.map((item) => ({
      ...item,
      outcome: "APPLIED",
      resultRevisionRef: "published",
      resultBindingRef: "binding",
      resultBindingVersion: 4,
      resultConsumerVersion: 3,
    })),
  };
  expect(checkedPublicationPage(result, plan).items).toHaveLength(1);
  for (const changes of [
    { resultRevisionRef: "foreign" },
    { resultBindingRef: "foreign" },
    { resultBindingVersion: 3 },
  ]) {
    expect(() =>
      checkedPublicationPage(
        {
          ...result,
          items: result.items.map((item) => ({ ...item, ...changes })),
        },
        plan,
      ),
    ).toThrow();
  }
});
it("не отправляет duplicate, expired или terminal selection", () => {
  expect(() => publicationSelection(plan, ["item", "item"])).toThrow();
  expect(() =>
    publicationSelection({ ...plan, expiresAt: "2000-01-01T00:00:00Z" }, []),
  ).toThrow();
  expect(() =>
    publicationSelection({ ...plan, state: "EXPIRED" }, []),
  ).toThrow();
});
