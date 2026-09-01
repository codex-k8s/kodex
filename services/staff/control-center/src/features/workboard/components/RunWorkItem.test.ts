import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./RunWorkItem.vue", import.meta.url),
  "utf8",
);

describe("RunWorkItem", () => {
  it("подписывает источник как Запущено через и показывает его справа от контекста", () => {
    expect(source).toContain('$t("common.source")');
    expect(source).toContain("sourceIcon");
    expect(source).toContain("runs.source.${run.source}");
    expect(source).toContain("run-work-item__aside");
  });
});
