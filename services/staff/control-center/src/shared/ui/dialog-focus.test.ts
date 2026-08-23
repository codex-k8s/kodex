import { describe, expect, it } from "vitest";

import { trappedFocusTarget } from "@/shared/ui/dialog-focus";

function element(name: string): HTMLElement {
  return { dataset: { name } } as unknown as HTMLElement;
}

describe("dialog focus trap", () => {
  const first = element("first");
  const middle = element("middle");
  const last = element("last");
  const elements = [first, middle, last];

  it("переносит Tab с последнего элемента на первый", () => {
    expect(trappedFocusTarget(elements, last, false)).toBe(first);
  });

  it("переносит Shift+Tab с первого элемента на последний", () => {
    expect(trappedFocusTarget(elements, first, true)).toBe(last);
  });

  it("возвращает внешний фокус внутрь диалога", () => {
    expect(trappedFocusTarget(elements, element("outside"), false)).toBe(first);
    expect(trappedFocusTarget(elements, element("outside"), true)).toBe(last);
  });

  it("не перехватывает последовательный переход внутри диалога", () => {
    expect(trappedFocusTarget(elements, middle, false)).toBeUndefined();
    expect(trappedFocusTarget(elements, middle, true)).toBeUndefined();
  });
});
