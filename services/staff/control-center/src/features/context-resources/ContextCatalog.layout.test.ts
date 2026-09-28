import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./ContextCatalog.vue", import.meta.url),
  "utf8",
);

describe("каталог контекстных ресурсов", () => {
  it("держит каталог в той же панели без бесполезного полноэкранного режима", () => {
    expect(source).toContain('class="context-catalog panel"');
    expect(source).not.toContain("Maximize2");
  });

  it("показывает компактную таблицу с типовой иконкой и сохраняет контекст сотрудника", () => {
    expect(source).toContain('class="context-catalog__table"');
    expect(source).toContain('class="context-row"');
    expect(source).toContain(
      "<EntityIcon :kind=\"kind === 'skills' ? 'SKILL' : 'MEMORY'\" />",
    );
    expect(source).toContain("query: agentRef ? { agentRef } : {},");
    expect(source).not.toContain("<code>{{ item.ref }}</code>");
  });
});
