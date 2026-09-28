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
      "platform.loadAudit(projectRef.value, query.value, pageSize.value)",
    );
    expect(source).toContain("pageSize.value");
    expect(source).toContain("useCursorInfiniteScroll");
    expect(template).toContain('ref="sentinel"');
    expect(template).not.toContain('$t("audit.loadMore")');
    expect(source).toContain("useAdaptiveCursorPageSize");
  });

  it("загружает только первую cursor-порцию до пересечения sentinel", () => {
    const loadBody = source.slice(
      source.indexOf("async function load()"),
      source.indexOf("function loadMore()"),
    );

    expect(loadBody.match(/loadAudit/g)).toHaveLength(1);
    expect(loadBody).not.toContain("loadMoreAudit");
    expect(loadBody).not.toContain("while");
  });

  it("позволяет выбрать область Проекта без потери остальных параметров URL", () => {
    expect(source).toContain(':load-page="accessProjectOptions"');
    expect(source).toContain('@update:model-value="selectProject"');
    expect(source).toContain("{ ...route.query, projectRef: next }");
    expect(source).toContain("watch(projectRef, () => {");
    expect(source).toContain("loadSelectedProject()");
  });

  it("различает подготовку публикации секрета и общее изменение черновика", () => {
    expect(source).toContain('const prefix = "runtime-secret-draft."');
    expect(source).toContain("actionSummary(event)");
    expect(source).toContain("event.safeSummary");
  });

  it("скрывает частые технические heartbeat по умолчанию и позволяет вернуть их", () => {
    expect(source).toContain('"controlplane.report_warm_runtime"');
    expect(source).toContain("!technicalActions.has(event.action)");
    expect(template).toContain('name="audit-show-technical"');
    expect(template).toContain('$t("audit.showTechnical")');
    expect(template).toContain(':empty="list.length === 0 && !hasMore"');
    expect(source).toContain('next.technical = "1"');
  });
});
