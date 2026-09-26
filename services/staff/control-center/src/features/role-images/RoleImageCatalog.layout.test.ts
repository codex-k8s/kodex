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
const editor = readFileSync(
  new URL("./RoleImageEditor.vue", import.meta.url),
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

  it("до выбора роли показывает действие, а не ошибку недоступности", () => {
    expect(editor).toContain('if (!ref) return t("roleImages.chooseRole")');
    expect(editor).toContain('t("roleImages.unknownRole")');
  });

  it("не предлагает полноэкранный каталог для короткого списка", () => {
    expect(catalog).toContain(
      "(store.projectTotal[projectRef] ?? items.length) > 6",
    );
    expect(catalog).toContain("store.projectNextPageToken[projectRef]");
  });

  it("после reload показывает persisted promotion, а не отсутствие artifact", () => {
    expect(editor).toContain("const promotionEvidenceState = computed");
    expect(editor).toContain("recipe.value?.promotedImageReady");
    expect(editor).toContain(':state="promotionEvidenceState"');
  });
});
