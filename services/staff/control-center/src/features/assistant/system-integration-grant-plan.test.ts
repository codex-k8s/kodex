import { describe, expect, it } from "vitest";
import type {
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidates,
  SystemAssistantIntegrationGrantCandidate,
} from "@/shared/api/generated/openapi/types.gen";
import {
  systemIntegrationGrantPlanCandidate,
  systemIntegrationGrantPlanOwner,
  systemIntegrationGrantAppliedCandidate,
  systemIntegrationGrantReceiptRef,
} from "./system-integration-grant-plan";
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
  organizationRef: "organization_test",
  systemAssistantRef: "assistant_test",
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
    type: "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
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
const page: SystemAssistantIntegrationGrantCandidates = {
  scopeKind: "ORGANIZATION",
  organizationRef: pins.organizationRef,
  assistantRef: pins.systemAssistantRef,
  assistantVersion: 3,
  connectionRef: pins.connectionRef,
  connectionVersion: 5,
  definitionVersion: pins.definitionVersion,
  definitionDigest: pins.definitionDigest,
  items: [candidate],
  total: 1,
  nextPageToken: "",
};

describe("owner-confirmed SYSTEM grant plan", () => {
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
      systemIntegrationGrantAppliedCandidate(
        operation(),
        fresh,
        applied,
        "grant_applied",
      ),
    ).toBe(true);
    expect(
      systemIntegrationGrantAppliedCandidate(
        operation(),
        fresh,
        applied,
        pins.connectionRef,
      ),
    ).toBe(false);
    expect(
      systemIntegrationGrantAppliedCandidate(
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
    expect(systemIntegrationGrantReceiptRef(plan, operation().ref)).toBe(
      "grant_applied",
    );
    expect(
      systemIntegrationGrantReceiptRef(plan, operation().ref, {
        ...receipt,
        planRef: "foreign_plan",
      }),
    ).toBeUndefined();
    expect(
      systemIntegrationGrantReceiptRef(plan, operation().ref, {
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
      systemIntegrationGrantPlanOwner(operation(), pins.organizationRef),
    ).toBe(true);
    for (const patch of [
      { projectRef: "project_test" },
      { assistantProfileRef: "profile_test" },
      { assistantScope: "PROJECT" },
      { organizationRef: "foreign" },
      { scopeKind: "PROJECT" },
      { systemAssistantRef: "other" },
      { grantVersion: 1 },
      { approvalPolicy: "NONE" },
    ]) {
      const current = operation();
      current.parameters = { ...current.parameters, ...patch };
      expect(
        systemIntegrationGrantPlanOwner(current, pins.organizationRef),
      ).toBe(false);
    }
    const forged = operation();
    forged.target = { ...forged.target, kind: "AGENT" };
    expect(systemIntegrationGrantPlanOwner(forged, pins.organizationRef)).toBe(
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
      edited && systemIntegrationGrantPlanOwner(edited, pins.organizationRef),
    ).toBe(true);
    expect(friendlyPlanOperationType(editable)).toBe(
      "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
    );
    editable.parametersText = "invalid";
    expect(friendlyPlanOperationType(editable)).toBe(
      "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
    );
  });
  it("не лечит дрейф опубликованного candidate или текущего grant", () => {
    expect(
      systemIntegrationGrantPlanCandidate(operation(), page, candidate),
    ).toBe(true);
    expect(
      systemIntegrationGrantPlanCandidate(
        operation(),
        { ...page, connectionVersion: 6 },
        candidate,
      ),
    ).toBe(false);
    expect(
      systemIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        currentGrantRef: "grant_new",
        currentGrantVersion: 1,
      }),
    ).toBe(false);
    expect(
      systemIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        currentGrantEnabled: true,
      }),
    ).toBe(false);
    expect(
      systemIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        grantable: false,
      }),
    ).toBe(false);
    expect(
      systemIntegrationGrantPlanCandidate(operation(), page, {
        ...candidate,
        capability: {
          ...candidate.capability,
          allowedApprovalPolicies: ["HUMAN_EACH_EFFECT"],
        },
      }),
    ).toBe(false);
  });
});
