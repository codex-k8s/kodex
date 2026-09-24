import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantIntegrationGrantPlanForm.vue", import.meta.url),
  "utf8",
);
const editor = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("форма разрешения интеграции в плане помощника", () => {
  it("сверяет подключение, получателя и разрешённого кандидата без секретов", () => {
    expect(source).toContain("getIntegrationConnection");
    expect(source).toContain("getAgent");
    expect(source).toContain("getWorkflow");
    expect(source).toContain("nextRecipient.projectRef !== projectRef");
    expect(source).toContain("capabilityCandidates");
    expect(source).toContain("candidate.value.grantable");
    expect(source).toContain("candidate.value.pins.connectionVersion");
    expect(source).toContain("existingGrant.value");
    expect(source).toContain("approvalScopeOptions");
    expect(source).toContain("validApprovalScopeSelection");
    expect(source).toContain('changed("approvalScopePaths"');
    expect(source).not.toContain("credentialValue");
    expect(editor).toContain("<AssistantIntegrationGrantPlanForm");
    expect(editor).toContain(
      "integrationGrantValidity.value[operation.value.ref]",
    );
    expect(editor).toContain("!integrationGrantTouched.value");
  });
});
