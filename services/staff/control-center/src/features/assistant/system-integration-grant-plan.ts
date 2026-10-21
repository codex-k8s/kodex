import type {
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidate,
  SystemAssistantIntegrationGrantCandidates,
} from "@/shared/api/generated/openapi/types.gen";
import {
  integrationGrantFieldsMatch,
  integrationGrantCandidateFieldsMatch,
  integrationGrantReceiptRef,
  integrationGrantAppliedFieldsMatch,
} from "./integration-grant-plan-fields";

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
export function isSystemAssistantGrantScope(
  value: unknown,
): value is "ORGANIZATION" {
  return value === "ORGANIZATION";
}

export function systemIntegrationGrantPlanOwner(
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
): boolean {
  const { parameters } = operation;
  return Boolean(
    organizationRef &&
    operation.type === "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" &&
    operation.action === "UPDATE" &&
    parameters.scopeKind === "ORGANIZATION" &&
    parameters.organizationRef === organizationRef &&
    typeof parameters.systemAssistantRef === "string" &&
    parameters.systemAssistantRef.length > 0 &&
    integrationGrantFieldsMatch(operation, readonlyKeys),
  );
}

export function systemIntegrationGrantPlanCandidate(
  operation: AssistantPlanOperationInput,
  page: SystemAssistantIntegrationGrantCandidates,
  candidate: SystemAssistantIntegrationGrantCandidate,
): boolean {
  const { parameters } = operation;
  return (
    page.organizationRef === parameters.organizationRef &&
    isSystemAssistantGrantScope(page.scopeKind) &&
    page.assistantRef === parameters.systemAssistantRef &&
    integrationGrantCandidateFieldsMatch(operation, page, candidate)
  );
}

export function systemIntegrationGrantReceiptRef(
  plan: AssistantPlan,
  operationRef: string,
  receipt: AssistantPlanReceipt | undefined = plan.receipt,
): string | undefined {
  return integrationGrantReceiptRef(
    plan,
    operationRef,
    "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
    receipt,
  );
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
    integrationGrantAppliedFieldsMatch(operation, page, candidate, grantRef)
  );
}
