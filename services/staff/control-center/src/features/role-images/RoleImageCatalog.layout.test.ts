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
  it("на любом экране отделяет lifecycle заголовок от статуса и не обрезает дату сборки", () => {
    const desktop = editor.slice(
      editor.indexOf(".image-lifecycle {"),
      editor.indexOf("@media (max-width: 1000px)"),
    );
    expect(desktop).toMatch(
      /\.lifecycle-step\s*\{[^}]*grid-template-columns: 28px minmax\(0, 1fr\);/s,
    );
    expect(desktop).toMatch(
      /\.lifecycle-step > \.status-badge\s*\{[^}]*grid-column: 2;[^}]*max-width: 100%;[^}]*white-space: normal;[^}]*overflow: visible;/s,
    );
    expect(desktop).toMatch(
      /\.lifecycle-step > div > span,\s*\.lifecycle-step > div > \.lifecycle-step__build-meta\s*\{[^}]*white-space: normal;[^}]*overflow: visible;[^}]*text-overflow: clip;/s,
    );
    expect(editor).toContain(
      '<small v-if="currentBuild" class="lifecycle-step__build-meta">',
    );
    expect(editor).toContain(
      "new Date(currentBuild.updatedAt).toLocaleString()",
    );
  });
  it("ограничивает видимую историю пятью карточками и оставляет доступную прокрутку", () => {
    expect(editor).toMatch(
      /class="build-history__scroll build-history__scroll--builds"\s+role="region"\s+:aria-label="t\('roleImages.buildHistory'\)"\s+tabindex="0"/,
    );
    expect(editor).toMatch(
      /\.build-history__scroll--builds\s*\{[^}]*max-height: min\(480px, 70dvh\);[^}]*overflow-x: hidden;[^}]*overflow-y: auto;/s,
    );
    expect(editor).toMatch(
      /\.build-history__scroll--builds \.build-row\s*\{[^}]*min-height: 96px;[^}]*overflow-wrap: anywhere;/s,
    );
    expect(editor).toMatch(
      /\.build-history__scroll--builds \.build-debug-action\s*\{[^}]*max-width: 100%;[^}]*min-height: 32px;[^}]*height: auto;[^}]*white-space: normal;/s,
    );
    expect(editor).toContain('v-for="build in builds"');
    expect(editor).not.toContain("builds.slice(");
  });
  it("на телефоне переносит lifecycle status на отдельную строку и оставляет место помощнику", () => {
    const mobile = editor.slice(editor.indexOf("@media (max-width: 640px)"));
    expect(mobile).toContain(
      "padding-bottom: calc(144px + env(safe-area-inset-bottom))",
    );
    expect(mobile).toMatch(
      /\.lifecycle-step\s*\{[^}]*grid-template-columns: 28px minmax\(0, 1fr\)/s,
    );
    expect(mobile).toMatch(
      /\.lifecycle-step\s*\{[^}]*padding-inline-end: 76px/s,
    );
    expect(mobile).toMatch(
      /\.lifecycle-step > \.status-badge\s*\{[^}]*grid-column: 1 \/ -1;[^}]*white-space: normal;[^}]*overflow: visible;/s,
    );
    expect(mobile).toMatch(
      /\.lifecycle-step > \.status-badge\s*\{[^}]*box-sizing: border-box;[^}]*width: 100%;/s,
    );
    expect(mobile).toMatch(
      /\.lifecycle-step > div > span\s*\{[^}]*white-space: normal;[^}]*overflow: visible;/s,
    );
  });
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

  it("локализует системное имя образа в каталоге и деталях", () => {
    expect(catalog).toContain("localizeServerMessage(recipe.name)");
    expect(editor).toContain("const recipeDisplayName = computed");
    expect(editor).toContain("localizeServerMessage(recipe.value.name)");
    expect(editor).toContain("{{ recipeDisplayName }}");
    expect(editor).toContain('v-model="nameFieldValue"');
    expect(editor).not.toContain("{{ recipe?.name");
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

  it("повторяет FAILED/EXPIRED и обновляет детали по realtime-событию", () => {
    expect(editor).toContain("platform.roleImageRealtimeRevision");
    expect(editor).toContain("store.loadDetail(projectRef, recipeRef, false)");
    expect(editor).not.toContain("scheduleBuildPolling");
    expect(editor).toContain("!buildIsActive(current) ||");
    expect(editor).toContain("buildIsActive(currentBuild)");
    expect(editor).toContain('t("roleImages.retryPending")');
  });
});
