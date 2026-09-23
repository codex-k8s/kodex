import type {
  AssistantConversation,
  AssistantPlan,
  AssistantPlanOperation,
  AssistantPlanOperationInput,
  AssistantPlanTarget,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";

function assistantAppliedResourceRef(
  plan: AssistantPlan,
  operationRef: string,
  operationType: string,
  targetKinds?: string | readonly string[],
): string | undefined {
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const receipt = plan.receipt;
  if (
    plan.state !== "APPLIED" ||
    !operation?.selected ||
    operation.type !== operationType ||
    (targetKinds !== undefined &&
      !(typeof targetKinds === "string"
        ? operation.target.kind === targetKinds
        : targetKinds.includes(operation.target.kind))) ||
    !receipt ||
    receipt.planRef !== plan.ref ||
    receipt.planRevision !== plan.revision ||
    receipt.outcome !== "APPLIED"
  )
    return undefined;
  const matching = receipt.operationReceipts.filter(
    (item) => item.operationRef === operationRef,
  );
  if (matching.length !== 1 || !matching[0]?.resourceRef) return undefined;
  return matching[0].resourceRef;
}

export function assistantRoleImageBuildTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; recipeRef: string } | undefined {
  const recipeRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_ROLE_IMAGE_RECIPE",
    "ROLE_IMAGE_RECIPE",
  );
  return plan.projectRef && recipeRef
    ? { projectRef: plan.projectRef, recipeRef }
    : undefined;
}

export function assistantEnvironmentDraftTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; draftRef: string } | undefined {
  const draftRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
    "RUNTIME_ENVIRONMENT_DRAFT",
  );
  return plan.projectRef && draftRef
    ? { projectRef: plan.projectRef, draftRef }
    : undefined;
}

export function assistantIntegrationConnectionTarget(
  plan: AssistantPlan,
  operationRef: string,
): { connectionRef: string } | undefined {
  const connectionRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_INTEGRATION_CONNECTION",
    "INTEGRATION_CONNECTION",
  );
  return connectionRef ? { connectionRef } : undefined;
}

export function assistantCreatedScheduleTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; scheduleRef: string } | undefined {
  const scheduleRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_SCHEDULE",
    "SCHEDULE",
  );
  return plan.projectRef && scheduleRef
    ? { projectRef: plan.projectRef, scheduleRef }
    : undefined;
}

export function assistantCreatedWorkflowTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; workflowRef: string } | undefined {
  const workflowRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_WORKFLOW",
    "WORKFLOW",
  );
  return plan.projectRef && workflowRef
    ? { projectRef: plan.projectRef, workflowRef }
    : undefined;
}

export function assistantLaunchedRunTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; runRef: string } | undefined {
  const runRef = assistantAppliedResourceRef(plan, operationRef, "LAUNCH_RUN");
  return plan.projectRef && runRef && /^[A-Za-z0-9_-]{8,96}$/.test(runRef)
    ? { projectRef: plan.projectRef, runRef }
    : undefined;
}

export function assistantAwaitingReply(
  conversation?: AssistantConversation,
): boolean {
  const latest = conversation?.turns.at(-1);
  return Boolean(
    latest &&
    (latest.state === "QUEUED" ||
      latest.state === "RUNNING" ||
      (latest.role === "USER" && latest.state === "COMPLETED")),
  );
}

export interface EditablePlanOperation {
  value: AssistantPlanOperationInput;
  beforeText: string;
  parametersText: string;
  afterText: string;
}

export type FriendlyPlanOperationType =
  | "CREATE_PROJECT"
  | "UPDATE_PROJECT"
  | "CREATE_AGENT"
  | "UPDATE_AGENT"
  | "CHANGE_CAPABILITY"
  | "CHANGE_INTEGRATION_GRANT"
  | "CREATE_WORKFLOW"
  | "CREATE_SCHEDULE"
  | "CREATE_RUNTIME_ENVIRONMENT_DRAFT"
  | "CREATE_ROLE_IMAGE_RECIPE"
  | "CREATE_INTEGRATION_CONNECTION"
  | "LAUNCH_RUN";

export function friendlyPlanOperationType(
  operation: EditablePlanOperation,
): FriendlyPlanOperationType | undefined {
  if (
    operation.value.type !== "CREATE_PROJECT" &&
    operation.value.type !== "UPDATE_PROJECT" &&
    operation.value.type !== "CREATE_AGENT" &&
    operation.value.type !== "UPDATE_AGENT" &&
    operation.value.type !== "CHANGE_CAPABILITY" &&
    operation.value.type !== "CHANGE_INTEGRATION_GRANT" &&
    operation.value.type !== "CREATE_WORKFLOW" &&
    operation.value.type !== "CREATE_SCHEDULE" &&
    operation.value.type !== "CREATE_RUNTIME_ENVIRONMENT_DRAFT" &&
    operation.value.type !== "CREATE_ROLE_IMAGE_RECIPE" &&
    operation.value.type !== "CREATE_INTEGRATION_CONNECTION" &&
    operation.value.type !== "LAUNCH_RUN"
  )
    return undefined;
  const expectedKind =
    operation.value.type === "CREATE_ROLE_IMAGE_RECIPE"
      ? "ROLE_IMAGE_RECIPE"
      : operation.value.type === "CREATE_INTEGRATION_CONNECTION"
        ? "INTEGRATION_CONNECTION"
        : operation.value.type === "CHANGE_INTEGRATION_GRANT"
          ? "INTEGRATION_CONNECTION"
          : operation.value.type === "CREATE_WORKFLOW"
            ? "WORKFLOW"
            : operation.value.type === "CREATE_SCHEDULE"
              ? "SCHEDULE"
              : operation.value.type.endsWith("PROJECT")
                ? "PROJECT"
                : operation.value.type === "CREATE_RUNTIME_ENVIRONMENT_DRAFT"
                  ? "RUNTIME_ENVIRONMENT_DRAFT"
                  : "AGENT";
  const expectedAction = operation.value.type.startsWith("CREATE_")
    ? "CREATE"
    : operation.value.type === "LAUNCH_RUN"
      ? "EXECUTE"
      : "UPDATE";
  if (
    (operation.value.type !== "LAUNCH_RUN" &&
      operation.value.target.kind !== expectedKind) ||
    operation.value.action !== expectedAction
  )
    return undefined;
  try {
    parseObject(operation.parametersText);
    parseObject(operation.beforeText);
    parseObject(operation.afterText);
    return operation.value.type;
  } catch {
    return undefined;
  }
}

export function operationParameter(
  operation: EditablePlanOperation,
  key: string,
): unknown {
  return parseObject(operation.parametersText)[key];
}

export function updateOperationParameter(
  operation: EditablePlanOperation,
  key: string,
  value: unknown,
): void {
  const parameters = parseObject(operation.parametersText);
  const after = parseObject(operation.afterText);
  parameters[key] = value;
  after[key] = value;
  operation.parametersText = prettyJSON(parameters);
  operation.afterText = prettyJSON(after);
  if (
    key === "name" &&
    operation.value.action === "CREATE" &&
    typeof value === "string"
  )
    operation.value.target.name = value;
}

function prettyJSON(value: Record<string, unknown>): string {
  return JSON.stringify(value, null, 2);
}

function cloneJSONRecord(
  value: Readonly<Record<string, unknown>>,
): Record<string, unknown> {
  return JSON.parse(JSON.stringify(value)) as Record<string, unknown>;
}

function cloneOperation(
  operation: AssistantPlanOperation,
): AssistantPlanOperationInput {
  return {
    ref: operation.ref,
    type: operation.type,
    action: operation.action,
    title: operation.title,
    summary: operation.summary,
    target: { ...operation.target },
    ...(operation.expectedVersion === undefined
      ? {}
      : { expectedVersion: operation.expectedVersion }),
    parameters: cloneJSONRecord(operation.parameters),
    before: cloneJSONRecord(operation.before),
    after: cloneJSONRecord(operation.after),
    selected: operation.selected,
    permitted: operation.permitted,
    ...(operation.unavailableReason === undefined
      ? {}
      : { unavailableReason: operation.unavailableReason }),
    validationProblems: [...operation.validationProblems],
  };
}

export function editableOperations(
  operations: readonly AssistantPlanOperation[],
): EditablePlanOperation[] {
  return operations.map((operation) => ({
    value: cloneOperation(operation),
    beforeText: prettyJSON(operation.before),
    parametersText: prettyJSON(operation.parameters),
    afterText: prettyJSON(operation.after),
  }));
}

function parseObject(value: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(value);
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed))
    throw new Error("JSON_OBJECT_REQUIRED");
  return parsed as Record<string, unknown>;
}

export function operationInputs(
  operations: readonly EditablePlanOperation[],
): AssistantPlanOperationInput[] {
  return operations.map((operation) => ({
    ...cloneOperation(operation.value),
    parameters: parseObject(operation.parametersText),
    before: parseObject(operation.beforeText),
    after: parseObject(operation.afterText),
  }));
}

export function operationActionLabel(
  action: AssistantPlanOperation["action"],
): "create" | "update" | "delete" | "execute" {
  if (action === "CREATE") return "create";
  if (action === "UPDATE") return "update";
  if (action === "ARCHIVE") return "delete";
  return "execute";
}

export function operationTargetLabel(target: AssistantPlanTarget): string {
  return target.name.trim() || target.kind.trim() || "—";
}

export function assistantEffectiveRuntimeState(
  assistant: SystemAssistant,
): SystemAssistant["runtimeState"] {
  if (
    assistant.runtimeState === "READY" &&
    !assistant.nextActions.includes("CREATE_CONVERSATION")
  )
    return "RECOVERING";
  return assistant.runtimeState;
}

export function assistantRequiresProviderAccount(
  assistant: SystemAssistant,
): boolean {
  return (
    !assistant.warmSessionRef &&
    !assistant.nextActions.includes("CREATE_CONVERSATION")
  );
}
