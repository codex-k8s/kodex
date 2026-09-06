import { describe, expect, it } from "vitest";
import { parse } from "smol-toml";
import { overlaySchemaFixture as schema } from "@/test-utils/runtime-catalog-fixture";
import {
  changeOverlayEffort,
  overlayCompletions,
  overlayEffort,
  overlayHover,
} from "./overlay-editor";
import {
  positionedCodeEditorDiagnostics,
  tomlCompletionQuery,
} from "./code-editor";
describe("Versioned overlay editor", () => {
  it("изменяет effort через TOML parser, сохраняя dotted и quoted поля", () => {
    const content = `"personality" = 'friendly'\n["history"]\npersistence = "none"\n`;
    const changed = changeOverlayEffort(content, schema, "low");
    expect(parse(changed)).toEqual({
      model_reasoning_effort: "low",
      personality: "friendly",
      history: { persistence: "none" },
    });
    expect(overlayEffort(changed, schema)).toBe("low");
    expect(parse(changeOverlayEffort(changed, schema, ""))).toEqual(
      parse(content),
    );
  });
  it.each([
    "personality = [",
    "model = 'server-owned'",
    "personality = 2023-02-30",
    "allow_login_shell = true",
    'personality = "\ud800"',
  ])("не нормализует недопустимый документ %s", (content) => {
    expect(() => changeOverlayEffort(content, schema, "low")).toThrow();
  });
  it("использует model-specific allowedValues и допускает исправление устаревшего effort", () => {
    expect(() => changeOverlayEffort("", schema, "max")).toThrow("unavailable");
    expect(overlayEffort('model_reasoning_effort = "max"', schema)).toBe("max");
    expect(
      overlayEffort(
        changeOverlayEffort('model_reasoning_effort = "max"', schema, ""),
        schema,
      ),
    ).toBe("");
  });
  it("completion использует owner поля, значения и действительную TOML table scope", () => {
    expect(
      overlayCompletions(schema, "", { content: "", cursor: 0 }).map(
        (item) => item.label,
      ),
    ).toEqual(schema.fields.map((field) => field.key));
    const content = '["history"]\nper';
    expect(
      overlayCompletions(schema, "per", { content, cursor: content.length }),
    ).toMatchObject([{ label: "persistence", apply: "persistence = " }]);
    const value = 'model_reasoning_effort = "h';
    expect(
      overlayCompletions(schema, '"h', {
        content: value,
        cursor: value.length,
      }).map((item) => item.label),
    ).toEqual(["low", "high"]);
    expect(
      overlayCompletions(schema, "", {
        content: 'personality = """\n',
        cursor: 18,
      }),
    ).toEqual([]);
  });
  it("hover возвращает только owner description и hover для точного ключа", () => {
    const content = '["history"]\npersistence = "none"';
    expect(overlayHover(schema, content, 17)?.text).toContain(
      schema.fields[3].hover,
    );
    expect(overlayHover(schema, 'model = "server"', 2)).toBeUndefined();
  });
  it("value completion заменяет полный quoted token, включая остаток справа", () => {
    const content = 'model_reasoning_effort = "high"';
    const cursor = content.indexOf("high") + 1;
    expect(tomlCompletionQuery(content, cursor, true)).toEqual({
      from: content.indexOf('"'),
      to: content.length,
      query: '"h',
    });
  });
  it("переводит UTF8 byte column в UTF16 marker, не повреждая emoji", () => {
    const content = "# comment\r\nключ = 😀bad";
    const column = new TextEncoder().encode("ключ = ").length + 1;
    const markers = positionedCodeEditorDiagnostics(
      [{ line: 2, column, message: "safe" }],
      content,
    );
    const normalized = content.replace(/\r\n/g, "\n");
    expect(markers).toEqual([
      {
        from: normalized.indexOf("😀"),
        to: normalized.indexOf("😀") + 2,
        severity: "error",
        message: "safe",
      },
    ]);
    expect(
      positionedCodeEditorDiagnostics(
        [
          { line: 0, column: 0, message: "safe" },
          { line: 2, column: 2, message: "inside UTF8 sequence" },
          { line: 3, column: 1, message: "outside" },
        ],
        content,
      ),
    ).toEqual([]);
  });
});
