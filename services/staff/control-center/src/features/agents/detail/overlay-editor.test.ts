import { describe, expect, it } from "vitest";
import { parse } from "smol-toml";
import { overlaySchemaFixture as schema } from "@/test-utils/runtime-catalog-fixture";
import {
  changeOverlayEffort,
  changeOverlayPreference,
  overlayPreference,
  overlayCompletions,
  overlayEffort,
  overlayHover,
} from "./overlay-editor";
import {
  positionedCodeEditorDiagnostics,
  tomlCompletionQuery,
} from "./code-editor";
describe("Versioned overlay editor", () => {
  it("изменяет native preferences без потери других настроек и возвращает наследование", () => {
    const content =
      'model_reasoning_effort = "high"\n[context]\nprune = false\n[cache]\noptimization = "NO_HINTS"';
    const changed = changeOverlayPreference(
      content,
      schema,
      "context.auto_compaction",
      "true",
    );
    expect(parse(changed)).toEqual({
      model_reasoning_effort: "high",
      context: { prune: false, auto_compaction: true },
      cache: { optimization: "NO_HINTS" },
    });
    expect(overlayPreference(changed, schema, "context.auto_compaction")).toBe(
      "true",
    );
    expect(
      overlayPreference(
        changeOverlayPreference(changed, schema, "context.auto_compaction", ""),
        schema,
        "context.auto_compaction",
      ),
    ).toBe("");
    expect(parse(changeOverlayEffort(changed, schema, "low"))).toMatchObject({
      context: { prune: false },
      cache: { optimization: "NO_HINTS" },
      model_reasoning_effort: "low",
    });
    expect(() =>
      changeOverlayPreference(changed, schema, "cache.no_store", "unknown"),
    ).toThrow();
    expect(() =>
      changeOverlayPreference(
        'cache.qualification = "SUPPORTED"',
        schema,
        "cache.no_store",
        "true",
      ),
    ).toThrow();
    expect(
      overlayPreference('context.prune = "false"', schema, "context.prune"),
    ).toBeUndefined();
    expect(
      overlayPreference("context.prune = ", schema, "context.prune"),
    ).toBeUndefined();
  });
  it("изменяет effort через TOML parser, сохраняя dotted и quoted поля", () => {
    const content = `"cache".optimization = 'NO_HINTS'\n["context"]\nprune = false\n`;
    const changed = changeOverlayEffort(content, schema, "low");
    expect(parse(changed)).toEqual({
      model_reasoning_effort: "low",
      cache: { optimization: "NO_HINTS" },
      context: { prune: false },
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
    const content = '["cache"]\nopt';
    expect(
      overlayCompletions(schema, "opt", { content, cursor: content.length }),
    ).toMatchObject([{ label: "optimization", apply: "optimization = " }]);
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
    const content = '["cache"]\noptimization = "NO_HINTS"';
    expect(overlayHover(schema, content, 17)?.text).toContain(
      schema.fields.find((field) => field.key === "cache.optimization")?.hover,
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
