import { describe, expect, it } from "vitest";

import {
  codeEditorDiagnostics,
  codeMirrorPhrases,
  templateCompletionQuery,
} from "./code-editor";

describe("CodeEditor boundary", () => {
  it("выделяет незавершённую template variable для server autocomplete", () => {
    expect(templateCompletionQuery("Роль {{project.na", 18, false)).toEqual({
      from: 5,
      query: "project.na",
    });
    expect(templateCompletionQuery("Обычный текст", 12, false)).toBeUndefined();
    expect(templateCompletionQuery("Обычный текст", 12, true)).toEqual({
      from: 12,
      query: "",
    });
  });

  it("преобразует server validation в безопасные CodeMirror diagnostics", () => {
    expect(codeEditorDiagnostics(["Недопустимый параметр"], 20)).toEqual([
      {
        from: 0,
        to: 1,
        severity: "error",
        message: "Недопустимый параметр",
      },
    ]);
    expect(codeEditorDiagnostics(["Пустой документ"], 0)[0]?.to).toBe(0);
  });

  it("локализует встроенные search, completion и diagnostics controls", () => {
    expect(codeMirrorPhrases("ru-RU")).toMatchObject({
      Find: "Найти",
      Completions: "Варианты дополнения",
      Diagnostics: "Диагностика",
    });
    expect(codeMirrorPhrases("en-US")).toEqual({});
  });
});
