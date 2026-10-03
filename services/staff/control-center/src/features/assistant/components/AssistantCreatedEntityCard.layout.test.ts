import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantCreatedEntityCard.vue", import.meta.url),
  "utf8",
);
const workspace = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);

describe("карточка созданного помощником проекта или сотрудника", () => {
  it("открывает только подтверждённый readback объект в его проекте", () => {
    expect(source).toContain("assistantCreatedEntityTarget");
    expect(source).toContain("getProject");
    expect(source).toContain("getAgent");
    expect(source).toContain("next.ref !== resourceRef");
    expect(source).toContain("getProjectAssistant");
    expect(source).toContain("profile.ref !== value.resourceRef");
    expect(source).toContain("profile.projectRef !== value.projectRef");
    expect(source).toContain("resourceRef = profile.agentRef");
    expect(source).toContain("agentRef: confirmed.ref");
    expect(source).toContain("next.projectRef !== value.projectRef");
    expect(source).toContain('v-if="entity && destination"');
    expect(workspace).toContain("<AssistantCreatedEntityCard");
    expect(workspace).toContain("item.type === 'CREATE_PROJECT'");
    expect(workspace).toContain("item.type === 'CREATE_AGENT'");
    expect(workspace).toContain("item.type === 'CREATE_PROJECT_ASSISTANT'");
    expect(source).toContain('query: { assistantForm: "1" }');
    expect(source).toContain('operation.type === "UPDATE_PROJECT"');
    expect(source).toContain('operation.type === "UPDATE_AGENT"');
    expect(source).toContain('updated ? "updatedTitle" : "title"');
    expect(source).not.toContain("emit('navigate')");
    const agentPage = readFileSync(
      new URL("../../../pages/AgentDetailPage.vue", import.meta.url),
      "utf8",
    );
    expect(agentPage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
    const projectPage = readFileSync(
      new URL("../../../pages/ProjectOverviewPage.vue", import.meta.url),
      "utf8",
    );
    expect(projectPage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
  });
});
