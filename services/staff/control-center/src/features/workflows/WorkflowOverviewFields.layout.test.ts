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

describe("основные поля процесса", () => {
  it("переиспользует редактор и проектный выбор координатора", () => {
    expect(manual).toContain("<WorkflowOverviewFields");
    expect(assistant).toContain("<WorkflowOverviewFields");
    expect(shared).toContain("<AsyncEntityPicker");
    expect(shared).toContain(':context-key="projectRef"');
    expect(shared).toContain("<VoiceTextarea");
  });
});
