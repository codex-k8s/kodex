import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantSchedulePlanForm.vue", import.meta.url),
  "utf8",
);
const editor = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("форма автоматизации в плане помощника", () => {
  it("показывает задание, точную цель и ближайшие запуски", () => {
    expect(source).toContain("automationText");
    expect(source).toContain("createExecutionTargetPickerLoader");
    expect(source).toContain("target.projectRef !== projectRef");
    expect(source).toContain("loadSchedulePreview");
    expect(source).toContain("previewIdentity.value === previewKey.value");
    expect(source).toContain("cronExpression");
    expect(source).toContain("automationTimezoneOptions");
    expect(source).toContain("formatAutomationOccurrence");
    expect(source).toContain("<AutomationPromptPreview");
    expect(source).toContain("automations.misfire");
    expect(source).toContain("automations.overlap");
  });

  it("не сбрасывает некорректный ввод процесса при изменении названия", () => {
    expect(source).toContain(
      'JSON.stringify([props.operation.value.ref, parameter("input")])',
    );
    expect(source).toContain("prepareAssistantWorkflowInput");
    expect(editor).toContain("<AssistantSchedulePlanForm");
    expect(editor).toContain("scheduleFormValidity.value[operation.value.ref]");
    expect(editor).toContain("!scheduleFormTouched.value");
  });
});
