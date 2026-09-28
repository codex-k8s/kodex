import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AdministrationPage.vue", import.meta.url),
  "utf8",
);

describe("Административные реестры", () => {
  it("показывает окружения и инциденты таблицами, не заменяя сводку", () => {
    expect(source).toContain("administration-table--environments");
    expect(source).toContain("administration-table--incidents");
    expect(source).toContain('<EntityIcon kind="ENVIRONMENT" />');
    expect(source).toContain('class="metric-grid"');
    expect(source).not.toContain('class="entity-list"');
  });
});
