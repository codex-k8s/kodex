import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const manual = readFileSync(
  new URL("../../pages/WorkflowDetailPage.vue", import.meta.url),
  "utf8",
);
const assistant = readFileSync(
  new URL(
    "../assistant/components/AssistantWorkflowPlanForm.vue",
    import.meta.url,
  ),
  "utf8",
);
const shared = readFileSync(
  new URL("./WorkflowOverviewFields.vue", import.meta.url),
  "utf8",
);
const create = readFileSync(
  new URL("../../pages/WorkflowsPage.vue", import.meta.url),
  "utf8",
);

describe("основные поля процесса", () => {
  it("переиспользует редактор и проектный выбор координатора", () => {
    expect(manual).toContain("<WorkflowOverviewFields");
    expect(assistant).toContain("<WorkflowOverviewFields");
    expect(shared).toContain("<AsyncEntityPicker");
    expect(shared).toContain(':context-key="projectRef"');
    expect(shared).toContain("<VoiceTextarea");
    expect(shared).toContain("align-content: start;");
    expect(shared.indexOf("workflows.concurrency")).toBeLessThan(
      shared.indexOf("workflows.completion"),
    );
  });

  it("не загружает весь список координаторов в форме создания", () => {
    expect(create).toContain("<AsyncEntityPicker");
    expect(create).toContain(':load-page="loadCoordinatorAgents"');
    expect(create).toContain("loadAgentCatalogPage");
    expect(create).not.toContain("platform.loadAgents(projectRef.value)");
    expect(create).not.toContain("<select");
  });

  it("в редакторе читает точные сохранённые назначения вне первой страницы", () => {
    expect(manual).toContain(
      "loadAssignedAgent(project, ref, controller.signal)",
    );
    expect(manual).toContain("assignedAgentUnavailable");
    expect(manual).not.toContain("platform.loadAgents(project)");
    expect(manual).toContain("loadAgentCatalogPage");
  });

  it("ставит редактор назначения и переменные рядом на desktop", () => {
    expect(manual).toContain('class="field--wide workflow-prompt-layout"');
    expect(manual).toContain('class="workflow-prompt-aside"');
    expect(manual).toContain(
      "grid-template-columns: minmax(0, 1fr) minmax(330px, 0.42fr);",
    );
    expect(manual).toContain(".workflow-prompt-editor :deep(.cm-editor)");
  });
});
