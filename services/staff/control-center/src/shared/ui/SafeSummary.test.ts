import { describe, expect, it } from "vitest";

import { safeSummary } from "@/shared/ui/safe-summary";

describe("safeSummary", () => {
  it("не выводит сырой JSON в списке", () => {
    expect(safeSummary('{"status":"blocked","score":null}')).toEqual({
      text: "",
      structured: true,
      truncated: false,
    });
  });

  it("удаляет markdown-разметку и opaque refs", () => {
    const result = safeSummary(
      "**Готово:** см. [`результат`](https://example.test) `run_secret123456`.",
    );

    expect(result.text).toBe("Готово: см. результат.");
    expect(result.text).not.toContain("run_secret123456");
    expect(result.text).not.toContain("https://");
  });

  it("ограничивает длинное описание целым словом", () => {
    const result = safeSummary("один два три четыре", 14);

    expect(result).toEqual({
      text: "один два три…",
      structured: false,
      truncated: true,
    });
  });
});
