import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AuditPage.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("AuditPage pagination", () => {
  it("использует server-side search и cursor-дозагрузку", () => {
    expect(source).toContain(
      "platform.loadAudit(projectRef.value, query.value)",
    );
    expect(source).toContain(
      "platform.loadMoreAudit(projectRef.value, query.value)",
    );
    expect(source).toContain("useCursorInfiniteScroll");
    expect(template).toContain('ref="sentinel"');
    expect(template).toContain('$t("audit.loadMore")');
  });

  it("ограничивает начальную загрузку двумя cursor-страницами", () => {
    const loadBody = source.slice(
      source.indexOf("async function load()"),
      source.indexOf("function loadMore()"),
    );

    expect(loadBody.match(/loadAudit/g)).toHaveLength(1);
    expect(loadBody.match(/loadMoreAudit/g)).toHaveLength(1);
    expect(loadBody).not.toContain("while");
  });
});
