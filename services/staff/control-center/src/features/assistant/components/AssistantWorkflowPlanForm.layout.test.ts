import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantWorkflowPlanForm.vue", import.meta.url),
  "utf8",
);
const editor = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("форма процесса в плане помощника", () => {
  it("редактирует этапы и входные поля без изменения prop", () => {
    expect(source).toContain("loadAgentCatalogPage");
    expect(source).toContain("getAgent");
    expect(source).toContain("agent.projectRef === projectRef");
    expect(source).toContain('change("steps"');
    expect(source).toContain('change("inputFields"');
    expect(source).not.toContain("props.operation.value.parameters =");
  });

  it("блокирует сохранение и повторное применение старой ревизии", () => {
    expect(editor).toContain("<AssistantWorkflowPlanForm");
    expect(editor).toContain("workflowFormValidity.value[operation.value.ref]");
    expect(editor).toContain("!workflowFormTouched.value");
    expect(editor).toContain("friendlyInputsReady.value");
  });
});
