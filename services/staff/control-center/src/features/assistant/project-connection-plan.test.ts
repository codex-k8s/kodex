import { describe, expect, it } from "vitest";
import type {
  AssistantPlan,
  AssistantPlanOperation,
} from "@/shared/api/generated/openapi/types.gen";
import { projectAssistantConnectionPlanOwner } from "./project-connection-plan";
import {
  assistantIntegrationConnectionTarget,
  editableOperations,
  friendlyPlanOperationType,
  operationInputs,
  updateOperationParameter,
} from "./model";

function operation(): AssistantPlanOperation {
  const pins = {
    projectAssistantRef: "agt_fixture12345",
    assistantScope: "PROJECT",
    scopeKind: "ORGANIZATION",
    organizationRef: "org_fixture12345",
    projectRef: "prj_fixture12345",
    assistantProfileRef: "asstp_fixture12345",
    agentVersion: 2,
    profileVersion: 1,
    definitionVersion: "2.3.1",
    definitionDigest: "a".repeat(64),
  };
  const parameters = {
    ...pins,
    definitionKey: "github",
    name: "Own repository",
    publicConfiguration: { owner: "fixture", repository: "repository" },
  };
  return {
    ref: "op_fixture12345",
    type: "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION",
    action: "CREATE",
    title: "Prepare connection",
    summary: "Owner confirmed",
    target: {
      kind: "PROJECT_ASSISTANT",
      ref: pins.projectAssistantRef,
      name: "Own helper",
      version: 2,
    },
    expectedVersion: 2,
    parameters,
    before: { ...pins, assistantName: "Own helper" },
    after: { ...parameters },
    selected: true,
    permitted: true,
    validationProblems: [],
  };
}
describe("project assistant connection preparation", () => {
  it("accepts explicit ORG ownership without deriving it from empty project", () => {
    expect(
      projectAssistantConnectionPlanOwner(operation(), "org_fixture12345"),
    ).toBe(true);
    expect(
      projectAssistantConnectionPlanOwner(operation(), "org_other12345"),
    ).toBe(false);
    const op = operation();
    op.parameters.projectRef = "";
    op.before.projectRef = "";
    op.after.projectRef = "";
    expect(projectAssistantConnectionPlanOwner(op)).toBe(false);
  });
  it.each([
    "organizationRef",
    "projectRef",
    "assistantProfileRef",
    "projectAssistantRef",
    "agentVersion",
    "profileVersion",
    "definitionDigest",
    "scopeKind",
  ])("rejects drift in immutable %s", (key) => {
    const op = operation();
    op.after[key] = "changed";
    expect(projectAssistantConnectionPlanOwner(op)).toBe(false);
  });
  it("edits public configuration/name preserving target/owner/Before", () => {
    const op = operation();
    const editable = editableOperations([op])[0];
    if (!editable) throw new Error("MISSING_FIXTURE");
    updateOperationParameter(editable, "name", "Another name");
    updateOperationParameter(editable, "publicConfiguration", {
      owner: "fixture",
      repository: "another",
    });
    const value = operationInputs([editable])[0];
    if (!value) throw new Error("MISSING_FIXTURE");
    expect(value.before).toEqual(op.before);
    expect(value.target).toEqual(op.target);
    expect(projectAssistantConnectionPlanOwner(value, "org_fixture12345")).toBe(
      true,
    );
    expect(friendlyPlanOperationType(editable)).toBe(op.type);
  });
  it("rejects caller credentials and malformed configuration", () => {
    const op = operation();
    op.parameters.credentials = "not-allowed";
    expect(projectAssistantConnectionPlanOwner(op)).toBe(false);
    const bad = operation();
    bad.parameters.publicConfiguration = { owner: 1 };
    bad.after.publicConfiguration = { owner: 1 };
    expect(projectAssistantConnectionPlanOwner(bad)).toBe(false);
  });
  it("links only an exact applied receipt to the protected credentials card", () => {
    const op = operation();
    const plan: AssistantPlan = {
      ref: "plan_fixture12345",
      conversationRef: "conv_fixture12345",
      projectRef: "prj_fixture12345",
      state: "APPLIED",
      version: 3,
      revision: 1,
      validatedRevision: 1,
      validationProblems: [],
      auditSummary: "Prepare",
      applied: true,
      contentDigest: "a".repeat(64),
      nextActions: [],
      operations: [op],
      receipt: {
        ref: "receipt_fixture12345",
        planRef: "plan_fixture12345",
        planRevision: 1,
        outcome: "APPLIED",
        operationReceipts: [
          {
            operationRef: op.ref,
            resourceRef: "icn_fixture12345",
            auditRef: "audit_fixture12345",
            outcome: "APPLIED",
          },
        ],
        conflicts: [],
        auditRefs: [],
        createdResourceRefs: ["icn_fixture12345"],
        createdAt: "2026-10-04T00:00:00Z",
      },
    };
    expect(assistantIntegrationConnectionTarget(plan, op.ref)).toEqual({
      connectionRef: "icn_fixture12345",
    });
    if (!plan.receipt) throw new Error("MISSING_FIXTURE");
    plan.receipt.planRevision = 2;
    expect(assistantIntegrationConnectionTarget(plan, op.ref)).toBeUndefined();
  });
});
