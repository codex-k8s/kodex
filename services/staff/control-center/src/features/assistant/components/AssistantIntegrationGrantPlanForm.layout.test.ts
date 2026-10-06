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
const bundle = readFileSync(
  new URL("../integration-grant-read-bundle.ts", import.meta.url),
  "utf8",
);

describe("форма разрешения интеграции в плане помощника", () => {
  it("сверяет подключение, получателя и разрешённого кандидата без секретов", () => {
    expect(source).toContain("createIntegrationGrantReadBundle");
    expect(source).toMatch(
      /\(props\.readBundle \?\? localReadBundle\)\s*\.read\(/,
    );
    expect(bundle).toContain("getIntegrationConnection");
    expect(bundle).toContain("getAgent");
    expect(bundle).toContain("getWorkflow");
    expect(bundle).toContain("recipient.projectRef !== projectRef");
    expect(bundle).toContain("connection.ref !== connectionRef");
    expect(bundle).toContain("recipient.ref !== recipientRef");
    expect(bundle).toContain("capabilityCandidates");
    expect(bundle).toContain(
      "page.pins.connectionVersion !== connectionVersion",
    );
    expect(bundle).toContain("ownerRequestSignal()");
    expect(bundle).toContain("subscriber.throwIfAborted()");
    expect(bundle).toContain("lifetime.throwIfAborted()");
    expect(source).toContain("candidate.value.grantable");
    expect(source).toContain("candidate.value.pins.connectionVersion");
    expect(source).toContain(
      "connection.value?.version === props.operation.value.expectedVersion",
    );
    expect(source).not.toContain('parameter("expectedVersion")');
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
    expect(source).toContain('v-if="!disabled && !versionMatches"');
    expect(source).toContain('v-if="problem"');
    expect(source).toContain("problem.value = true");
    expect(editor).toContain(':read-bundle="grantReadBundle"');
  });
  it("компактный header остаётся display:contents поверх позднего общего display:flex, номер в своей колонке", () => {
    expect(editor).toMatch(
      /\.assistant-plan-operation\.assistant-plan-operation--compact-grant > header\s*{\s*display: contents;/,
    );
    expect(editor).toMatch(
      /\.assistant-plan-operation--compact-grant \.assistant-plan-operation__number\s*{\s*grid-column: 3;\s*grid-row: 1;/,
    );
    expect(editor).toMatch(
      /\.assistant-plan-operation--compact-grant > \.assistant-plan-friendly\s*{\s*grid-column: 2;\s*grid-row: 1;/,
    );
  });
});
