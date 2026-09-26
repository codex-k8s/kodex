import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("TemplateVariableCatalog server filters", () => {
  it("передаёт выбранную область серверу и обновляет cursor-каталог", () => {
    const source = readFileSync(
      new URL("./TemplateVariableCatalog.vue", import.meta.url),
      "utf8",
    );

    expect(source).toContain(
      'source: activeScope.value === "ALL" ? undefined : activeScope.value',
    );
    expect(source.match(/source: activeScope\.value/g)).toHaveLength(2);
    expect(source).toContain(
      'watch(activeScope, () => refresh(), { flush: "sync" })',
    );
    expect(source).not.toContain(
      "items.value.filter((item) => item.scope === activeScope.value)",
    );
  });
});
