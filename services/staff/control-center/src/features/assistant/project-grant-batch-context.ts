import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";
import { projectIntegrationGrantPlanOwner } from "./project-integration-grant-plan";

export interface ProjectGrantBatchContext {
  key: string;
  connectionName: string;
}

export function projectGrantBatchContext(
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
  connectionName: string | undefined,
): ProjectGrantBatchContext | undefined {
  if (
    !connectionName?.trim() ||
    !projectIntegrationGrantPlanOwner(operation, organizationRef)
  )
    return undefined;
  const parameters = operation.parameters;
  return {
    connectionName,
    key: JSON.stringify([
      parameters.organizationRef,
      parameters.scopeKind,
      parameters.assistantScope,
      parameters.projectRef,
      parameters.assistantProfileRef,
      parameters.projectAssistantRef,
      parameters.agentVersion,
      parameters.profileVersion,
      parameters.connectionRef,
      operation.expectedVersion,
      parameters.definitionVersion,
      parameters.definitionDigest,
      parameters.enabled,
      parameters.approvalPolicy,
      [...(parameters.approvalScopePaths as string[])].sort(),
    ]),
  };
}

export function commonProjectGrantBatchContext(
  operations: AssistantPlanOperationInput[],
  contexts: Record<string, ProjectGrantBatchContext | undefined>,
  organizationRef: string | undefined,
): ProjectGrantBatchContext | undefined {
  if (operations.length < 2) return undefined;
  const firstOperation = operations[0];
  if (!firstOperation) return undefined;
  const first = contexts[firstOperation.ref];
  if (!first) return undefined;
  return operations.every((operation) => {
    const context = contexts[operation.ref];
    if (!context) return false;
    const current = projectGrantBatchContext(
      operation,
      organizationRef,
      context.connectionName,
    );
    return (
      context.key === first.key &&
      current?.key === first.key &&
      context.connectionName === first.connectionName
    );
  })
    ? first
    : undefined;
}
