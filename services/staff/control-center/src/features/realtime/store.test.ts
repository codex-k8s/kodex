import { describe, expect, it } from "vitest";

import { reducePlatformSequence } from "@/features/realtime/store";

describe("reducePlatformSequence", () => {
  it("применяет только следующую org sequence", () => {
    expect(reducePlatformSequence(8, 9)).toBe("applied");
  });

  it("игнорирует at-least-once duplicate", () => {
    expect(reducePlatformSequence(8, 8)).toBe("duplicate");
    expect(reducePlatformSequence(8, 7)).toBe("duplicate");
  });

  it("обнаруживает gap и некорректную sequence", () => {
    expect(reducePlatformSequence(8, 10)).toBe("gap");
    expect(reducePlatformSequence(-1, 1)).toBe("invalid");
    expect(reducePlatformSequence(0, 0)).toBe("invalid");
  });
});
