import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";

// grantPlanOperationFixture сохраняет точный тип операции и независимые scope pins caller.
export function grantPlanOperationFixture(
  type:
    | "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT"
    | "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
  connectionRef: string,
  before: AssistantPlanOperationInput["before"],
  after: AssistantPlanOperationInput["after"],
): AssistantPlanOperationInput {
  return {
    ref: "operation_test",
    type,
    action: "UPDATE",
    title: "Context7",
    summary: "Доступ к документации",
    target: {
      kind: "INTEGRATION_CONNECTION",
      ref: connectionRef,
      version: 10,
      name: "Context7",
    },
    expectedVersion: 10,
    selected: true,
    permitted: true,
    validationProblems: [],
    parameters: after,
    before,
    after,
  };
}

// grantCandidateFixture создаёт отдельный синтетический READ capability для каждого теста.
export function grantCandidateFixture(capabilityKey: string) {
  return {
    capability: {
      key: capabilityKey,
      name: "Документация",
      description: "",
      risk: "READ",
      approvalRequired: false,
      operation: "READ",
      resourceKind: "MANAGED_MCP",
      inputFields: [],
      approvalPolicy: "NONE",
      allowedApprovalPolicies: ["NONE"],
    },
    grantable: true,
    reason: "READY",
    currentGrantVersion: 0,
    currentGrantEnabled: false,
    currentApprovalScopePaths: [],
  };
}
