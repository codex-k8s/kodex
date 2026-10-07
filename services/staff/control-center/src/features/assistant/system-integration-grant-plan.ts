import type {
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantCandidates,
} from "@/shared/api/generated/openapi/types.gen";

const readonlyKeys = [
  "connectionRef",
  "capabilityKey",
  "scopeKind",
  "organizationRef",
  "systemAssistantRef",
  "definitionVersion",
  "definitionDigest",
  "grantRef",
  "grantVersion",
  "defaultApprovalPolicy",
  "allowedApprovalPolicies",
] as const;
const editableKeys = [
  "enabled",
  "approvalPolicy",
  "approvalScopePaths",
] as const;
const policies = ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"];
const equal = (left: unknown, right: unknown) =>
  JSON.stringify(left) === JSON.stringify(right);
export function isSystemAssistantGrantScope(
  value: unknown,
): value is "ORGANIZATION" {
  return value === "ORGANIZATION";
}

export function systemIntegrationGrantPlanOwner(
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
): boolean {
  const { parameters, before, after, target } = operation;
  const allowed = parameters.allowedApprovalPolicies;
  const fields = new Set<string>([...readonlyKeys, ...editableKeys]);
  return Boolean(
    organizationRef &&
    operation.type === "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" &&
    operation.action === "UPDATE" &&
    parameters.scopeKind === "ORGANIZATION" &&
    parameters.organizationRef === organizationRef &&
    typeof parameters.systemAssistantRef === "string" &&
    parameters.systemAssistantRef.length > 0 &&
    typeof parameters.connectionRef === "string" &&
    parameters.connectionRef.length > 0 &&
    typeof parameters.capabilityKey === "string" &&
    parameters.capabilityKey.length > 0 &&
    target.kind === "INTEGRATION_CONNECTION" &&
    target.ref === parameters.connectionRef &&
    Number.isSafeInteger(operation.expectedVersion) &&
    (operation.expectedVersion ?? 0) >= 1 &&
    target.version === operation.expectedVersion &&
    typeof parameters.definitionVersion === "string" &&
    parameters.definitionVersion.length > 0 &&
    typeof parameters.definitionDigest === "string" &&
    /^[a-f0-9]{64}$/.test(parameters.definitionDigest) &&
    typeof parameters.grantRef === "string" &&
    Number.isSafeInteger(parameters.grantVersion) &&
    (parameters.grantVersion as number) >= 0 &&
    !!parameters.grantRef === (parameters.grantVersion as number) > 0 &&
    Array.isArray(allowed) &&
    allowed.length > 0 &&
    new Set(allowed).size === allowed.length &&
    allowed.every(
      (policy) => typeof policy === "string" && policies.includes(policy),
    ) &&
    allowed.includes(parameters.defaultApprovalPolicy) &&
    [parameters, before, after].every((snapshot) =>
      Object.keys(snapshot).every((key) => fields.has(key)),
    ) &&
    readonlyKeys.every(
      (key) =>
        equal(parameters[key], before[key]) &&
        equal(parameters[key], after[key]),
    ) &&
    editableKeys.every((key) => equal(parameters[key], after[key])) &&
    typeof parameters.enabled === "boolean" &&
    typeof before.enabled === "boolean" &&
    typeof parameters.approvalPolicy === "string" &&
    allowed.includes(parameters.approvalPolicy) &&
    typeof before.approvalPolicy === "string" &&
    policies.includes(before.approvalPolicy) &&
    [parameters.approvalScopePaths, before.approvalScopePaths].every(
      (paths) =>
        Array.isArray(paths) &&
        paths.length <= 16 &&
        new Set(paths).size === paths.length &&
        paths.every(
          (path) =>
            typeof path === "string" && path.length > 0 && path.length <= 160,
        ),
    ),
  );
}

export function systemIntegrationGrantPlanCandidate(
  operation: AssistantPlanOperationInput,
  page: SystemAssistantIntegrationGrantCandidates,
  candidate: SystemAssistantIntegrationGrantCandidate,
): boolean {
  const { parameters, before } = operation;
  return (
    page.organizationRef === parameters.organizationRef &&
    isSystemAssistantGrantScope(page.scopeKind) &&
    page.assistantRef === parameters.systemAssistantRef &&
    page.connectionRef === parameters.connectionRef &&
    page.connectionVersion === operation.expectedVersion &&
    page.definitionVersion === parameters.definitionVersion &&
    page.definitionDigest === parameters.definitionDigest &&
    candidate.capability.key === parameters.capabilityKey &&
    candidate.grantable &&
    (candidate.currentGrantRef ?? "") === parameters.grantRef &&
    candidate.currentGrantVersion === parameters.grantVersion &&
    candidate.currentGrantEnabled === before.enabled &&
    (candidate.currentApprovalPolicy ?? candidate.capability.approvalPolicy) ===
      before.approvalPolicy &&
    equal(candidate.currentApprovalScopePaths, before.approvalScopePaths) &&
    candidate.capability.approvalPolicy === parameters.defaultApprovalPolicy &&
    equal(
      candidate.capability.allowedApprovalPolicies,
      parameters.allowedApprovalPolicies,
    )
  );
}

export function systemIntegrationGrantReceiptRef(
  plan: AssistantPlan,
  operationRef: string,
  receipt: AssistantPlanReceipt | undefined = plan.receipt,
): string | undefined {
  const operation = plan.operations.find((item) => item.ref === operationRef);
  if (
    plan.state !== "APPLIED" ||
    operation?.type !== "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" ||
    !operation.selected ||
    receipt?.outcome !== "APPLIED" ||
    receipt.planRef !== plan.ref ||
    receipt.planRevision !== plan.revision
  )
    return undefined;
  const matches = receipt.operationReceipts.filter(
    (item) =>
      item.operationRef === operationRef &&
      (["APPLIED"] as readonly string[]).includes(item.outcome),
  );
  return matches.length === 1 && matches[0]?.resourceRef
    ? matches[0].resourceRef
    : undefined;
}

export function systemIntegrationGrantAppliedCandidate(
  operation: AssistantPlanOperationInput,
  page: SystemAssistantIntegrationGrantCandidates,
  candidate: SystemAssistantIntegrationGrantCandidate,
  grantRef: string,
): boolean {
  const { parameters } = operation;
  return (
    !!grantRef &&
    isSystemAssistantGrantScope(page.scopeKind) &&
    page.organizationRef === parameters.organizationRef &&
    page.assistantRef === parameters.systemAssistantRef &&
    page.connectionRef === parameters.connectionRef &&
    page.connectionVersion >=
      (operation.expectedVersion ?? Number.POSITIVE_INFINITY) &&
    page.definitionVersion === parameters.definitionVersion &&
    page.definitionDigest === parameters.definitionDigest &&
    candidate.capability.key === parameters.capabilityKey &&
    candidate.currentGrantRef === grantRef &&
    candidate.currentGrantVersion >= 1 &&
    candidate.currentGrantEnabled === parameters.enabled &&
    candidate.currentApprovalPolicy === parameters.approvalPolicy &&
    equal(candidate.currentApprovalScopePaths, parameters.approvalScopePaths)
  );
}
