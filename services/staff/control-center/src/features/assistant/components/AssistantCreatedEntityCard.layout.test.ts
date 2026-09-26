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
    expect(source).toContain("next.ref !== value.resourceRef");
    expect(source).toContain("next.projectRef !== value.projectRef");
    expect(source).toContain('v-if="entity && destination"');
    expect(workspace).toContain("<AssistantCreatedEntityCard");
    expect(workspace).toContain("item.type === 'CREATE_PROJECT'");
    expect(workspace).toContain("item.type === 'CREATE_AGENT'");
    expect(source).toContain('query: { assistantForm: "1" }');
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
