import { describe, expect, it } from "vitest";
import type {
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperationInput,
  ProjectAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import {
  projectIntegrationGrantPlanCandidate,
  projectIntegrationGrantPlanOwner,
  projectIntegrationGrantAppliedCandidate,
  projectIntegrationGrantReceiptRef,
} from "./project-integration-grant-plan";
import {
  editableOperations,
  friendlyPlanOperationType,
  operationInputs,
  updateOperationParameter,
} from "./model";

const pins = {
  connectionRef: "connection_test",
  capabilityKey: "create",
  scopeKind: "ORGANIZATION",
  assistantScope: "PROJECT",
  projectRef: "project_test",
  assistantProfileRef: "profile_test",
  agentVersion: 3,
  profileVersion: 1,
  organizationRef: "organization_test",
  projectAssistantRef: "assistant_test",
  definitionVersion: "1.0",
  definitionDigest: "a".repeat(64),
  grantRef: "",
  grantVersion: 0,
  defaultApprovalPolicy: "HUMAN_EACH_EFFECT",
  allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
};
const before = {
  ...pins,
  enabled: false,
  approvalPolicy: "HUMAN_EACH_EFFECT",
  approvalScopePaths: [],
};
function operation(): AssistantPlanOperationInput {
  const after = {
    ...pins,
    enabled: true,
    approvalPolicy: "HUMAN_EACH_EFFECT",
    approvalScopePaths: [],
  };
  return {
    ref: "operation_system_grant",
    type: "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
    action: "UPDATE",
    title: "Доступ",
    summary: "Подтвердить доступ",
    target: {
      kind: "INTEGRATION_CONNECTION",
      ref: pins.connectionRef,
      name: "GitHub",
      version: 5,
    },
    selected: true,
    permitted: true,
    validationProblems: [],
    expectedVersion: 5,
    parameters: structuredClone(after),
    before: structuredClone(before),
    after: structuredClone(after),
  };
}
const candidate: SystemAssistantIntegrationGrantCandidate = {
  capability: {
    key: "create",
    name: "Создание",
    description: "",
    risk: "WRITE",
    approvalRequired: true,
    operation: "CREATE",
    resourceKind: "GITHUB_REPOSITORY",
    inputFields: [],
    approvalPolicy: "HUMAN_EACH_EFFECT",
    allowedApprovalPolicies: ["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"],
  },
  grantable: true,
  reason: "READY",
  currentGrantVersion: 0,
  currentGrantEnabled: false,
  currentApprovalScopePaths: [],
};
const page: ProjectAssistantIntegrationGrantCandidates = {
  scopeKind: "ORGANIZATION",
  organizationRef: pins.organizationRef,
  projectRef: pins.projectRef,
  assistantProfileRef: pins.assistantProfileRef,
  assistantRef: pins.projectAssistantRef,
  assistantVersion: 3,
  profileVersion: 1,
  connectionRef: pins.connectionRef,
  connectionVersion: 5,
  definitionVersion: pins.definitionVersion,
  definitionDigest: pins.definitionDigest,
  items: [candidate],
  total: 1,
  nextPageToken: "",
};

describe("owner-confirmed PROJECT grant plan", () => {
  it("проверяет APPLIED grant по resourceRef квитанции, а не по connection locator", () => {
    const applied = {
      ...candidate,
      currentGrantRef: "grant_applied",
      currentGrantVersion: 1,
      currentGrantEnabled: true,
      currentApprovalPolicy: "HUMAN_EACH_EFFECT" as const,
    };
    const fresh = { ...page, connectionVersion: 6, items: [applied] };
    expect(
      projectIntegrationGrantAppliedCandidate(
        operation(),
        fresh,
        applied,
        "grant_applied",
      ),
    ).toBe(true);
    expect(
      projectIntegrationGrantAppliedCandidate(
        operation(),
        fresh,
        applied,
        pins.connectionRef,
      ),
    ).toBe(false);
    expect(
      projectIntegrationGrantAppliedCandidate(
        operation(),
        fresh,
        { ...applied, currentApprovalPolicy: "NONE" },
        "grant_applied",
      ),
    ).toBe(false);
    const receipt: AssistantPlanReceipt = {
      ref: "receipt_test",
      planRef: "plan_test",
      planRevision: 2,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: operation().ref,
          resourceRef: "grant_applied",
          outcome: "APPLIED",
          auditRef: "audit_test",
        },
      ],
      conflicts: [],
      auditRefs: [],
      createdResourceRefs: ["grant_applied"],
      createdAt: "2026-10-04T00:00:00Z",
    };
    const plan = {
      ref: "plan_test",
      state: "APPLIED",
      revision: 2,
      operations: [operation()],
      receipt,
    } as AssistantPlan;
    expect(projectIntegrationGrantReceiptRef(plan, operation().ref)).toBe(
      "grant_applied",
    );
    expect(
      projectIntegrationGrantReceiptRef(plan, operation().ref, {
        ...receipt,
        planRef: "foreign_plan",
      }),
    ).toBeUndefined();
    expect(
      projectIntegrationGrantReceiptRef(plan, operation().ref, {
        ...receipt,
        operationReceipts: [
          ...receipt.operationReceipts,
          ...receipt.operationReceipts,
        ],
      }),
    ).toBeUndefined();
  });
  it("принимает exact readonly pins и rejects authority fields", () => {
    expect(
      projectIntegrationGrantPlanOwner(operation(), pins.organizationRef),
    ).toBe(true);
    for (const patch of [
      { projectRef: "foreign_project" },
      { assistantProfileRef: "foreign_profile" },
      { assistantScope: "SYSTEM" },
      { organizationRef: "foreign" },
      { scopeKind: "PROJECT" },
      { projectAssistantRef: "other" },
      { grantVersion: 1 },
      { approvalPolicy: "NONE" },
    ]) {
      const current = operation();
      current.parameters = { ...current.parameters, ...patch };
      expect(
        projectIntegrationGrantPlanOwner(current, pins.organizationRef),
      ).toBe(false);
    }
    const forged = operation();
    forged.target = { ...forged.target, kind: "AGENT" };
    expect(projectIntegrationGrantPlanOwner(forged, pins.organizationRef)).toBe(
      false,
    );
  });
  it("редактирует только policy/enabled/paths и сохраняет исходный before", () => {
    const original = operation();
    const editable = editableOperations([
      { ...original, permitted: true, validationProblems: [] },
    ])[0];
    if (!editable) throw new Error("Missing operation fixture");
    updateOperationParameter(editable, "approvalPolicy", "HUMAN_SCOPED");
    updateOperationParameter(editable, "approvalScopePaths", ["/repository"]);
    updateOperationParameter(editable, "enabled", false);
    const edited = operationInputs([editable])[0];
    expect(edited?.before).toEqual(before);
    expect(edited?.parameters.approvalPolicy).toBe("HUMAN_SCOPED");
    expect(edited?.after.approvalScopePaths).toEqual(["/repository"]);
    expect(
      edited && projectIntegrationGrantPlanOwner(edited, pins.organizationRef),
    ).toBe(true);
    expect(friendlyPlanOperationType(editable)).toBe(
      "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
    );
    editable.parametersText = "invalid";
    expect(friendlyPlanOperationType(editable)).toBe(
      "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
    );
  });
  it("не лечит дрейф опубликованного candidate или текущего grant", () => {
    expect(
      projectIntegrationGrantPlanCandidate(operation(), page, candidate),
    ).toBe(true);
    expect(
      projectIntegrationGrantPlanCandidate(
        operation(),
        { ...page, connectionVersion: 6 },
        candidate,
      ),
    ).toBe(false);
    expect(
      projectIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        currentGrantRef: "grant_new",
        currentGrantVersion: 1,
      }),
    ).toBe(false);
    expect(
      projectIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        currentGrantEnabled: true,
      }),
    ).toBe(false);
    expect(
      projectIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        grantable: false,
      }),
    ).toBe(false);
    expect(
      projectIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        capability: {
          ...candidate.capability,
          allowedApprovalPolicies: ["HUMAN_EACH_EFFECT"],
        },
      }),
    ).toBe(false);
  });
});
