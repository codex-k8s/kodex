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
    expect(lineage).toContain("roleImages.managedBy.${lineage.managedBy}");
    expect(lineage).not.toContain("{{ lineage?.managedBy ??");
  });

  it("до выбора роли показывает действие, а не ошибку недоступности", () => {
    expect(editor).toContain('if (!ref) return t("roleImages.chooseRole")');
    expect(editor).toContain('t("roleImages.unknownRole")');
  });

  it("не дублирует каталог образов в полноэкранной модалке", () => {
    expect(catalog).not.toContain("catalog.expand");
    expect(catalog).not.toContain("Maximize2");
    expect(catalog).toContain("useCursorInfiniteScroll");
    expect(catalog).toContain("useAdaptiveCursorPageSize");
  });

  it("не предлагает разворот пустой или короткой истории образа", () => {
    expect(editor).toContain('v-if="!buildsExpanded && builds.length > 5"');
    expect(editor).toContain("revisions.length > 5 ||");
    expect(editor).toContain("store.revisionNextPageToken[recipe.ref]");
  });

  it("после reload показывает persisted promotion, а не отсутствие artifact", () => {
    expect(editor).toContain("const promotionEvidenceState = computed");
    expect(editor).toContain("recipe.value?.promotedImageReady");
    expect(editor).toContain(':state="promotionEvidenceState"');
  });

  it("предлагает диагностику последней неуспешной сборки рядом со статусом образа", () => {
    const summary = editor.slice(
      editor.indexOf('class="image-summary__actions"'),
      editor.indexOf('class="editor-layout"'),
    );
    expect(summary).toContain("currentBuild.stage");
    expect(summary).toContain("'DEAD_LETTER'");
    expect(summary).toContain(
      '@click="requestAssistantRoleImageBuildDebug(currentBuild)"',
    );
    expect(summary).toContain('t("roleImages.debugBuildWithAssistant")');
  });

  it("повторяет FAILED/EXPIRED и опрашивает допуск после завершения сборки", () => {
    expect(editor).toContain("roleImageLifecycleNeedsRefresh(");
    expect(editor).toContain("pollingPaused.value");
    expect(editor).toContain("!buildIsActive(current) ||");
    expect(editor).toContain("buildIsActive(currentBuild)");
    expect(editor).toContain('t("roleImages.retryPending")');
  });
});
