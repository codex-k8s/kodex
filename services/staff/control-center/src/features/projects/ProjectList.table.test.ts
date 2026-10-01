import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const list = readFileSync(
  new URL("./ProjectList.vue", import.meta.url),
  "utf8",
);
const page = readFileSync(
  new URL("../../pages/ProjectsPage.vue", import.meta.url),
  "utf8",
);

describe("Таблица проектов и корзины", () => {
  it("ставит название с иконкой первым и оставляет действия для обоих состояний", () => {
    expect(list).toContain('<EntityIcon kind="PROJECT" />');
    expect(list).toContain('<th scope="col">{{ $t("common.name") }}</th>');
    expect(list).toContain("project-list__table--trash");
    expect(list).toContain("emit('trash', project)");
    expect(list).toContain("emit('restore', project)");
    expect(list).toContain("emit('purge', project)");
    expect(list).not.toContain("<article");
  });

  it("считает курсор по строкам, а не по карточкам в двух колонках", () => {
    expect(page).toContain('itemSelector: ".project-list__item"');
    expect(page).toContain("estimatedColumns: 1");
  });
});
