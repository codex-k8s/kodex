import { describe, expect, it } from "vitest";

import {
  hasCompleteRunSnapshot,
  reducePlatformSequence,
} from "@/features/realtime/store";

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

describe("hasCompleteRunSnapshot", () => {
  it("принимает snapshot только вместе с последним событием timeline", () => {
    const graph = { sequence: 28 } as never;
    expect(hasCompleteRunSnapshot(graph, { 28: {} as never }, 28)).toBe(true);
    expect(hasCompleteRunSnapshot(graph, { 21: {} as never }, 28)).toBe(false);
  });

  it("принимает пустой новый Run с cursor 0", () => {
    expect(hasCompleteRunSnapshot({ sequence: 0 } as never, {}, 0)).toBe(true);
  });
});
