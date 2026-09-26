import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const catalog = readFileSync(
  new URL("./RoleImageCatalog.vue", import.meta.url),
  "utf8",
);
const lineage = readFileSync(
  new URL("./RoleImageLineage.vue", import.meta.url),
  "utf8",
);

describe("каталог образов ИИ-сотрудников", () => {
  it("скрывает внутренние ссылки конфигурации в технических сведениях", () => {
    expect(catalog).toContain("<RoleImageLineage");
    expect(catalog).toContain("collapsible");
    expect(lineage).toContain(":is=\"collapsible ? 'details' : 'div'\"");
    expect(lineage).toContain("roleImages.technicalDetails");
    expect(lineage).toContain("lineage.configurationRef");
    expect(lineage).toContain("lineage.revisionRef");
  });
});
