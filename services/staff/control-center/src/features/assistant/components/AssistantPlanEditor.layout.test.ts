import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("AssistantPlanEditor layout", () => {
  it("показывает тип, действие и authority результата без скрытых изменений", () => {
    expect(source).toContain("{{ operation.value.type }}");
    expect(source).toContain("{{ operation.value.action }}");
    expect(source).toContain("{{ operation.value.permitted }}");
    expect(source).toContain("operation.value.target.kind");
    expect(source).toContain("operation.value.target.name");
    expect(source).toContain("operation.value.target.ref");
    expect(source).toContain("operation.value.target.version");
    expect(source).toContain("operation.value.expectedVersion");
  });

  it("даёт открыть parameters, before и after в расширенном редакторе", () => {
    expect(source).toContain("kind: 'PARAMETERS'");
    expect(source).toContain("kind: 'BEFORE'");
    expect(source).toContain("kind: 'AFTER'");
    expect(source).toContain("<AssistantCodeEditorModal");
  });

  it("не разрешает редактировать уже применённый или отклонённый план", () => {
    expect(source).toContain('["APPLIED", "REJECTED"].includes');
    expect(source).toContain(':disabled="!editable"');
  });
});
