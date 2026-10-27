import { describe, expect, it } from "vitest";

import { hasInvalidImpactPageEnvelope } from "./impact-page-validation";

describe("границы страницы impact-плана", () => {
  const plan = { ref: "plan", state: "PREPARED", total: 2 };
  const page = { plan, total: 2, items: [{ ref: "a" }], nextPageToken: "next" };
  const identity = (value: typeof plan) => value.ref;

  it("сохраняет ограничение cursor и допускает смену состояния первой страницы", () => {
    const applied = { ...page, plan: { ...plan, state: "APPLIED" } };
    expect(hasInvalidImpactPageEnvelope(applied, plan, identity)).toBe(false);
    expect(hasInvalidImpactPageEnvelope(applied, plan, identity, "old")).toBe(
      true,
    );
    expect(hasInvalidImpactPageEnvelope(page, plan, identity, "next")).toBe(
      true,
    );
    expect(hasInvalidImpactPageEnvelope(page, plan, identity, "")).toBe(false);
  });

  it("отклоняет несовпадение плана, недостоверный total и повторы ref", () => {
    const invalid = [
      { ...page, plan: { ...plan, ref: "other" } },
      { ...page, total: NaN },
      { ...page, total: 0 },
      { ...page, total: 3 },
      { ...page, items: [{ ref: "a" }, { ref: "a" }] },
    ];
    expect(hasInvalidImpactPageEnvelope(page, plan, identity)).toBe(false);
    for (const item of invalid)
      expect(hasInvalidImpactPageEnvelope(item, plan, identity)).toBe(true);
  });
});
