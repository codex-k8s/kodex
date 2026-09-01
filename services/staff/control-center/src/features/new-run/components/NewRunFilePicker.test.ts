import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./NewRunFilePicker.vue", import.meta.url),
  "utf8",
);

describe("NewRunFilePicker layout", () => {
  it("использует серверный async picker с multi-select и windowed virtualization", () => {
    expect(source).toContain(':load-items="loadItems"');
    expect(source).toContain("multiple");
    expect(source).toContain("virtualize");
    expect(source).toContain(':virtual-item-height="virtualItemHeight"');
    expect(source).toContain(":virtual-columns=");
  });

  it("сохраняет grid/list режимы и одинаковую высоту действий", () => {
    expect(source).toContain("<ViewModeToggle");
    expect(source).toContain("async-picker__virtual-items");
    expect(source).toContain(".new-run-file-picker__action");
    expect(source).toContain("min-height: 44px");
  });
});
