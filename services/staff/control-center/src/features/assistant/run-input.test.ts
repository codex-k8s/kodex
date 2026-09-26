import { describe, expect, it } from "vitest";

import type { WorkflowInputField } from "@/shared/api/generated/openapi/types.gen";
import { prepareAssistantWorkflowInput } from "./run-input";

function field(
  key: string,
  valueType: WorkflowInputField["valueType"],
  required = false,
  options: string[] = [],
): WorkflowInputField {
  return { key, label: key, description: "", valueType, required, options };
}

describe("входные данные запуска процесса через помощника", () => {
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
