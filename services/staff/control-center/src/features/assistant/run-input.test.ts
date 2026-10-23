import { describe, expect, it } from "vitest";

import type { WorkflowInputField } from "@/shared/api/generated/openapi/types.gen";
import {
  assistantWorkflowInputDraft,
  prepareAssistantWorkflowInput,
} from "./run-input";

function field(
  key: string,
  valueType: WorkflowInputField["valueType"],
  required = false,
  options: string[] = [],
): WorkflowInputField {
  return { key, label: key, description: "", valueType, required, options };
}

describe("входные данные запуска процесса через помощника", () => {
  it("сохраняет скалярный ввод и помечает сложные значения черновика", () => {
    expect(
      assistantWorkflowInputDraft({
        text: " исходный ",
        count: 2,
        enabled: false,
        empty: null,
        nested: { value: 1 },
        list: [1],
        missing: undefined,
      }),
    ).toEqual({
      rawInput: {
        text: " исходный ",
        count: "2",
        enabled: "false",
        empty: "null",
        nested: "[object Object]",
        list: "1",
        missing: "undefined",
      },
      invalidKeys: new Set(["nested", "list", "missing"]),
    });
  });

  it.each([undefined, null, "text", 5, ["value"]])(
    "не принимает %j за объект начального ввода",
    (input) => {
      expect(assistantWorkflowInputDraft(input)).toEqual({
        rawInput: {},
        invalidKeys: new Set(),
      });
    },
  );

  it("нормализует типы и обязательные поля", () => {
    expect(
      prepareAssistantWorkflowInput(
        [
          field("name", "TEXT", true),
          field("count", "NUMBER"),
          field("enabled", "BOOLEAN"),
          field("kind", "SELECT", false, ["report", "review"]),
          field("day", "DATE"),
        ],
        { name: "  test  ", count: "2.5", kind: "report", day: "2024-02-29" },
      ),
    ).toEqual({
      value: {
        name: "test",
        count: 2.5,
        enabled: false,
        kind: "report",
        day: "2024-02-29",
      },
      problems: {},
    });
  });

  it.each([
    [{ enabled: "garbage" }, "enabled"],
    [{ count: "Infinity" }, "count"],
    [{ kind: "unknown" }, "kind"],
    [{ day: "2026-02-30" }, "day"],
    [{ unknown: "value" }, "unknown"],
  ])("закрыто отклоняет некорректный ввод %j", (input, problem) => {
    const result = prepareAssistantWorkflowInput(
      [
        field("enabled", "BOOLEAN"),
        field("count", "NUMBER"),
        field("kind", "SELECT", false, ["report"]),
        field("day", "DATE"),
      ],
      input,
    );
    expect(result.problems[problem]).toBe(true);
  });

  it("требует заполнить обязательный текст", () => {
    expect(
      prepareAssistantWorkflowInput([field("name", "TEXT", true)], {}).problems,
    ).toEqual({ name: true });
  });
});
