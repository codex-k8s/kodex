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
const disclosure = readFileSync(
  new URL("../../workflows/WorkflowStepDisclosure.vue", import.meta.url),
  "utf8",
);
const workflowPage = readFileSync(
  new URL("../../../pages/WorkflowDetailPage.vue", import.meta.url),
  "utf8",
);

describe("форма процесса в плане помощника", () => {
  it("обе формы используют lazy шаги с видимыми ошибками и ограниченным списком", () => {
    expect(source).toContain("<WorkflowStepDisclosure");
    expect(workflowPage).toContain("<WorkflowStepDisclosure");
    expect(disclosure).toContain('v-if="open"');
    expect(disclosure).toContain('v-if="needsAttention"');
    expect(disclosure).toContain('role="status"');
    expect(source).toContain("max-height: min(68vh, 720px)");
    expect(workflowPage).toContain("max-height: min(68vh, 720px)");
    expect(disclosure).toContain("minmax(0, 1fr)");
    expect(disclosure).toContain("overflow-wrap: anywhere");
  });
  it("редактирует этапы и входные поля без изменения prop", () => {
    expect(source).toContain("loadAgentCatalogPage");
    expect(source).toContain("getAgent");
    expect(source).toContain("agent.projectRef === projectRef");
    expect(source).toContain('change("steps"');
    expect(source).toContain('change("inputFields"');
    expect(source).not.toContain("props.operation.value.parameters =");
  });

  it("использует штатные редакторы шаблона и каталог полномочий этапа", () => {
    expect(source).toContain("<TemplateSourceField");
    expect(source).toContain("<EffectiveCapabilityCatalog");
    expect(source).toContain('mode="REQUIREMENTS"');
    expect(source).toContain(
      "step.agentRef === agentRef ? step.requiredCapabilityKeys : []",
    );
    expect(source).not.toContain("step.requiredCapabilityKeys.join('\\n')");
  });

  it("блокирует сохранение и повторное применение старой ревизии", () => {
    expect(editor).toContain("<AssistantWorkflowPlanForm");
    expect(editor).toContain("operation.value.type === 'UPDATE_WORKFLOW'");
    expect(source).toContain(
      'text(parameter("workflowRef")) === props.operation.value.target.ref',
    );
    expect(source).toContain("text(field.key) || index");
    expect(source).toContain("text(step.key) || index");
    expect(editor).toContain("workflowFormValidity.value[operation.value.ref]");
    expect(editor).toContain("!workflowFormTouched.value");
    expect(editor).toContain("friendlyInputsReady.value");
  });
});
