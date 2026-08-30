import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantCodeEditorModal.vue", import.meta.url),
  "utf8",
);

describe("AssistantCodeEditorModal", () => {
  it("использует настоящий CodeMirror и освобождает EditorView", () => {
    expect(source).toContain('from "@codemirror/view"');
    expect(source).toContain("new EditorView({");
    expect(source).toContain("view?.destroy()");
  });

  it("открывает широкую modal и включает JSON language", () => {
    expect(source).toContain('size="xl"');
    expect(source).toContain("StreamLanguage.define(json)");
    expect(source).toContain("validateAssistantObjectJSON(draft.value)");
  });
});
