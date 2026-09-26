import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const panel = readFileSync(
  new URL("./BindingsPanel.vue", import.meta.url),
  "utf8",
);
const page = readFileSync(
  new URL("../../../pages/AccessPage.vue", import.meta.url),
  "utf8",
);

describe("BindingsPanel list contract", () => {
  it("передаёт серверу поиск, состояние и адаптивный размер cursor-страницы", () => {
    expect(panel).toContain('name="access-binding-search"');
    expect(panel).toContain("useAdaptiveCursorPageSize");
    expect(panel).toContain("useCursorInfiniteScroll");
    expect(panel).toContain('stateFilter.value === "ALL"');
    expect(panel).toContain('emit("search"');
    expect(panel).toContain('"more",');
    expect(page).toContain("query, includeRevoked, pageSize");
    expect(page).toContain("access.loadBindings(");
  });
});
