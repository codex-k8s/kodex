import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantLaunchRunForm.vue", import.meta.url),
  "utf8",
);
const editor = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("дружественная форма плана запуска", () => {
  it("выбирает только разрешённого исполнителя в текущем проекте", () => {
    expect(source).toContain("createExecutionTargetPickerLoader");
    expect(source).toContain("isEligibleAgent");
    expect(source).toContain("isEligibleWorkflow");
    expect(source).toContain("target.projectRef !== projectRef");
    expect(source).toContain("<AsyncEntityPicker");
    expect(editor).toContain("<AssistantLaunchRunForm");
    expect(editor).toContain("friendlyInputsReady.value");
  });

  it("показывает задачу и типизированные входы процесса, не меняя prop", () => {
    expect(source).toContain('stringParameter("task")');
    expect(source).toContain("workflow.inputFields");
    expect(source).toContain('emit("parameter", "input", prepared.value)');
    expect(source).not.toContain("props.operation.value.target.kind =");
    expect(editor).toContain("!runFormTouched.value");
  });
});
