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
    expect(source).toContain("min-height: 46px");
  });
});
