import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("TemplateVariableCatalog server filters", () => {
  it("передаёт выбранную область серверу и обновляет cursor-каталог", () => {
    const source = readFileSync(
      new URL("./TemplateVariableCatalog.vue", import.meta.url),
      "utf8",
    );

    expect(source).toContain(
      'source: activeScope.value === "ALL" ? undefined : activeScope.value',
    );
    expect(source.match(/source: activeScope\.value/g)).toHaveLength(2);
    expect(source).toContain(
      'watch(activeScope, () => refresh(), { flush: "sync" })',
    );
    expect(source).not.toContain(
      "items.value.filter((item) => item.scope === activeScope.value)",
    );
  });

  it("копирует переменную в предпросмотре, сохраняя вставку в редакторе", () => {
    const catalog = readFileSync(
      new URL("./TemplateVariableCatalog.vue", import.meta.url),
      "utf8",
    );
    const preview = readFileSync(
      new URL("./PromptTargetPreview.vue", import.meta.url),
      "utf8",
    );
    const instructions = readFileSync(
      new URL("./AgentInstructionsPanel.vue", import.meta.url),
      "utf8",
    );

    expect(catalog).toContain('action?: "insert" | "copy"');
    expect(preview).toContain('action="copy"');
    expect(preview).toContain("navigator.clipboard.writeText(");
    expect(preview).toContain('size="xl"');
    expect(preview).toContain('class="prompt-target-preview__sections"');
    expect(instructions).toContain('@select="insertVariable"');
  });
});
