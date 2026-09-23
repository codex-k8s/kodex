import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantCreatedWorkflowCard.vue", import.meta.url),
  "utf8",
);
const workspace = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);

describe("карточка созданного помощником процесса", () => {
  it("связывает процесс с точной квитанцией и авторитетным readback", () => {
    expect(source).toContain("assistantCreatedWorkflowTarget");
    expect(source).toContain("getWorkflow");
    expect(source).toContain("next.ref !== value.workflowRef");
    expect(source).toContain("next.projectRef !== value.projectRef");
    expect(source).toContain("workflow.launchReadiness.allowedToSubmit");
    expect(workspace).toContain("<AssistantCreatedWorkflowCard");
    expect(workspace).toContain("item.type === 'CREATE_WORKFLOW'");
  });
});
