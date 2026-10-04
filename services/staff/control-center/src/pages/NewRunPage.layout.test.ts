import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./NewRunPage.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("NewRunPage layout", () => {
  it("сохраняет контекст Проекта в форме, summary и обратных ссылках", () => {
    expect(template).toContain('class="project-context"');
    expect(template).toContain("project?.name");
    expect(template).toContain("`/projects/${projectRef}`");
    expect(template).toContain("`/projects/${projectRef}/files`");
  });

  it("использует доступную radio-группу Session и отдельный async picker", () => {
    expect(template).toContain("<NewRunSessionPolicy");
    expect(template).toContain("<NewRunSessionPicker");
    expect(template).toContain("sessionMode === 'CONTINUE'");
  });

  it("выравнивает Запустить и Отмена одной layout-группой", () => {
    expect(template).toContain('class="launch-summary__actions"');
    expect(source).toContain(".launch-summary__actions .button");
    expect(source).toContain("min-height: var(--control-height)");
  });

  it("очистка цели также убирает её старое имя из сводки запуска", () => {
    expect(template).toContain('@update:model-value="updateTargetRef"');
    expect(source).toContain("selectedTargetValue.value?.ref !== ref");
    expect(source).toContain("selectedTargetValue.value = undefined");
  });

  it("оставляет название необязательным и показывает источник запуска", () => {
    expect(template).toContain("runs.newRun.titleOptionalHint");
    expect(template).toContain("runs.newRun.titleWillBeSuggested");
    expect(template).toContain("runs.newRun.initiatorAndSource");
    expect(template).toContain("runs.newRun.manualSource");
    expect(template).toContain("runs.newRun.externalChannelUnavailable");
    expect(source).not.toContain("Boolean(form.title.trim())");
  });

  it("не подменяет отсутствие выбора файлов отсутствием файлов в Проекте", () => {
    expect(source).toContain('t("runs.chooseTargetBeforeFiles")');
    expect(template).toContain("attachmentEligibilityMessage");
    expect(source).toContain('t("runs.noInputFiles")');
  });
});
