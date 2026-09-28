import { describe, expect, it } from "vitest";

import { adaptiveCursorPageSize } from "./cursor-list";

describe("adaptiveCursorPageSize", () => {
  it("возвращает точный динамический размер, а не дискретные варианты", () => {
    expect(adaptiveCursorPageSize(731, 61, 1)).toBe(15);
    expect(adaptiveCursorPageSize(731, 61, 3)).toBe(45);
  });

  it("ограничивает слишком короткий и слишком большой viewport", () => {
    expect(adaptiveCursorPageSize(20, 100, 1)).toBe(8);
    expect(adaptiveCursorPageSize(100_000, 10, 4)).toBe(100);
  });
});
