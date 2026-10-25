import { describe, expect, it } from "vitest";
import type { TokenUsage } from "@/shared/api/generated/openapi/types.gen";
import { usageCompleteness } from "./token-usage";

describe("закрытая полнота расхода", () => {
  it("сохраняет UNKNOWN legacy и все три wire-значения", () => {
    expect(usageCompleteness({} as TokenUsage)).toBe("UNKNOWN");
    for (const quality of ["UNKNOWN", "PARTIAL", "COMPLETE"] as const)
      expect(usageCompleteness({ completeness: quality } as TokenUsage)).toBe(
        quality,
      );
  });
  it.each([null, 0, false, {}, [], "", "partial", "INVALID"])(
    "отвергает неверное значение %j",
    (value) => {
      expect(() =>
        usageCompleteness({ completeness: value } as unknown as TokenUsage),
      ).toThrow("Token usage completeness is invalid");
    },
  );
});
