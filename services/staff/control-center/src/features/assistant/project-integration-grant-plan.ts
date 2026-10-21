import type {
  AssistantPlan,
  AssistantPlanReceipt,
  AssistantPlanOperationInput,
  SystemAssistantIntegrationGrantCandidate,
  ProjectAssistantIntegrationGrantCandidates,
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
  "assistantScope",
  "projectRef",
  "assistantProfileRef",
  "agentVersion",
  "profileVersion",
  "organizationRef",
  "projectAssistantRef",
  "definitionVersion",
  "definitionDigest",
  "grantRef",
  "grantVersion",
  "defaultApprovalPolicy",
  "allowedApprovalPolicies",
] as const;
export function isProjectAssistantConnectionScope(
  value: unknown,
): value is "ORGANIZATION" {
  return value === "ORGANIZATION";
}

export function projectIntegrationGrantPlanOwner(
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
): boolean {
  const { parameters } = operation;
  return Boolean(
    organizationRef &&
    operation.type === "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT" &&
    operation.action === "UPDATE" &&
    parameters.scopeKind === "ORGANIZATION" &&
    parameters.assistantScope === "PROJECT" &&
    typeof parameters.projectRef === "string" &&
    !!parameters.projectRef &&
    typeof parameters.assistantProfileRef === "string" &&
    !!parameters.assistantProfileRef &&
    Number.isSafeInteger(parameters.agentVersion) &&
    (parameters.agentVersion as number) >= 1 &&
    Number.isSafeInteger(parameters.profileVersion) &&
    (parameters.profileVersion as number) >= 1 &&
    parameters.organizationRef === organizationRef &&
    typeof parameters.projectAssistantRef === "string" &&
    parameters.projectAssistantRef.length > 0 &&
    integrationGrantFieldsMatch(operation, readonlyKeys),
  );
}

export function projectIntegrationGrantPlanCandidate(
  operation: AssistantPlanOperationInput,
  page: ProjectAssistantIntegrationGrantCandidates,
  candidate: SystemAssistantIntegrationGrantCandidate,
): boolean {
  const { parameters } = operation;
  return (
    page.organizationRef === parameters.organizationRef &&
    isProjectAssistantConnectionScope(page.scopeKind) &&
    page.assistantRef === parameters.projectAssistantRef &&
    page.projectRef === parameters.projectRef &&
    page.assistantProfileRef === parameters.assistantProfileRef &&
    page.assistantVersion === parameters.agentVersion &&
    page.profileVersion === parameters.profileVersion &&
    integrationGrantCandidateFieldsMatch(operation, page, candidate)
  );
}

export function projectIntegrationGrantReceiptRef(
  plan: AssistantPlan,
  operationRef: string,
  receipt: AssistantPlanReceipt | undefined = plan.receipt,
): string | undefined {
  return integrationGrantReceiptRef(
    plan,
    operationRef,
    "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
    receipt,
  );
}

export function projectIntegrationGrantAppliedCandidate(
  operation: AssistantPlanOperationInput,
  page: ProjectAssistantIntegrationGrantCandidates,
  candidate: SystemAssistantIntegrationGrantCandidate,
  grantRef: string,
): boolean {
  const { parameters } = operation;
  return (
    !!grantRef &&
    isProjectAssistantConnectionScope(page.scopeKind) &&
    page.organizationRef === parameters.organizationRef &&
    page.assistantRef === parameters.projectAssistantRef &&
    page.projectRef === parameters.projectRef &&
    page.assistantProfileRef === parameters.assistantProfileRef &&
    page.assistantVersion === parameters.agentVersion &&
    page.profileVersion === parameters.profileVersion &&
    integrationGrantAppliedFieldsMatch(operation, page, candidate, grantRef)
  );
}
