import type {
  AssistantConversation,
  AssistantPlan,
  AssistantPlanOperation,
  AssistantPlanOperationInput,
  AssistantPlanTarget,
  AssistantTurn,
  Run,
  SystemAssistant,
} from "@/shared/api/generated/openapi/types.gen";
import {
  assertRunOwner,
  runSessionStorageBlocker,
} from "@/features/runs/run-owner";
import { projectAssistantConnectionPlanOwner } from "./project-connection-plan";
import {
  isProjectFileOperation,
  projectFileRevisionSource,
} from "./project-file-plan";

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

const projectAssistantOperationTypes = [
  "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
  "CREATE_INSTRUCTION_DRAFT",
  "BIND_AGENT_RUNTIME_ENVIRONMENT",
] as const;
const projectAssistantOwnerKeys = [
  "projectAssistantRef",
  "assistantScope",
  "scopeKind",
  "organizationRef",
  "projectRef",
  "assistantProfileRef",
  "agentVersion",
  "runtimeEnvironmentBindingRef",
  "runtimeEnvironmentVersionRef",
  "runtimeEnvironmentDigest",
] as const;

export function hasAssistantProjectHelperLocator(
  operation: Pick<
    AssistantPlanOperationInput,
    "parameters" | "before" | "after"
  >,
): boolean {
  return [operation.parameters, operation.before, operation.after].some(
    (value) =>
      [
        "projectAssistantRef",
        "assistantProfileRef",
        "assistantScope",
        "runtimeEnvironmentBindingRef",
        "runtimeEnvironmentVersionRef",
        "runtimeEnvironmentDigest",
      ].some((key) => Object.hasOwn(value, key)),
  );
}

export function assistantProjectHelperScope(
  _plan: Pick<AssistantPlan, "projectRef">,
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
): { projectRef: string; agentRef: string; profileRef: string } | undefined {
  const { parameters, before, after, target } = operation;
  const opaque = (value: unknown): value is string =>
    typeof value === "string" && /^[A-Za-z0-9_-]{8,128}$/.test(value);
  if (
    !organizationRef ||
    !opaque(organizationRef) ||
    !projectAssistantOperationTypes.some((type) => type === operation.type) ||
    operation.action !== "UPDATE" ||
    parameters.organizationRef !== organizationRef ||
    parameters.scopeKind !== "PROJECT" ||
    parameters.assistantScope !== "PROJECT" ||
    !opaque(parameters.projectAssistantRef) ||
    !opaque(parameters.projectRef) ||
    !opaque(parameters.assistantProfileRef) ||
    projectAssistantOwnerKeys.some(
      (key) =>
        parameters[key] !== before[key] || parameters[key] !== after[key],
    ) ||
    [parameters, before, after].some((value) =>
      Object.hasOwn(value, "systemAssistantRef"),
    ) ||
    !Number.isSafeInteger(parameters.agentVersion) ||
    (parameters.agentVersion as number) < 1 ||
    !opaque(parameters.runtimeEnvironmentBindingRef) ||
    !opaque(parameters.runtimeEnvironmentVersionRef) ||
    typeof parameters.runtimeEnvironmentDigest !== "string" ||
    !/^[a-f0-9]{64}$/.test(parameters.runtimeEnvironmentDigest) ||
    !Number.isSafeInteger(target.version) ||
    (target.version ?? 0) < 1 ||
    target.version !== operation.expectedVersion
  )
    return;
  if (operation.type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION") {
    if (
      target.kind !== "ENVIRONMENT" ||
      !opaque(target.ref) ||
      target.ref !== parameters.environmentRef ||
      target.ref !== before.environmentRef ||
      target.ref !== after.environmentRef
    )
      return;
  } else if (
    target.kind !== "AGENT" ||
    target.ref !== parameters.projectAssistantRef ||
    target.version !== parameters.agentVersion ||
    parameters.agentRef !== target.ref ||
    before.agentRef !== target.ref ||
    (operation.type === "CREATE_INSTRUCTION_DRAFT" &&
      after.agentRef !== target.ref)
  )
    return;
  return {
    projectRef: parameters.projectRef,
    agentRef: parameters.projectAssistantRef,
    profileRef: parameters.assistantProfileRef,
  };
}

export function assistantOperationProjectRef(
  plan: Pick<AssistantPlan, "projectRef">,
  operation: AssistantPlanOperationInput,
  organizationRef?: string,
): string | undefined {
  return hasAssistantProjectHelperLocator(operation)
    ? assistantProjectHelperScope(plan, operation, organizationRef)?.projectRef
    : plan.projectRef;
}

export function editableAssistantOperationProjectRef(
  plan: AssistantPlan,
  operation: EditablePlanOperation,
  organizationRef?: string,
): string | undefined {
  try {
    const current = operationInputs([operation])[0];
    const original = plan.operations.find(
      (value) => value.ref === operation.value.ref,
    );
    if (!current || !original) return;
    if (
      !hasAssistantProjectHelperLocator(current) &&
      !hasAssistantProjectHelperLocator(original)
    )
      return plan.projectRef;
    if (
      projectAssistantOwnerKeys.some(
        (key) =>
          current.parameters[key] !== original.parameters[key] ||
          current.before[key] !== original.before[key] ||
          current.after[key] !== original.after[key],
      ) ||
      current.target.ref !== original.target.ref ||
      current.target.kind !== original.target.kind ||
      current.target.version !== original.target.version ||
      current.expectedVersion !== original.expectedVersion
    )
      return;
    return assistantOperationProjectRef(plan, current, organizationRef);
  } catch {
    return;
  }
}

export function assistantRoleImageBuildTarget(
  plan: AssistantPlan,
  operationRef: string,
  organizationRef?: string,
):
  | { projectRef: string; recipeRef: string; resourceScope?: never }
  | {
      resourceScope: { kind: "ORGANIZATION"; organizationRef: string };
      recipeRef: string;
      projectRef?: never;
    }
  | undefined {
  const operation = plan.operations.find((item) => item.ref === operationRef);
  if (
    operation?.type === "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" ||
    operation?.type === "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
  ) {
    const recipeRef = assistantAppliedResourceRef(
      plan,
      operationRef,
      operation.type,
      "ROLE_IMAGE_RECIPE",
    );
    if (
      !recipeRef ||
      !organizationRef ||
      !systemAssistantImageOperationScope(operation, organizationRef) ||
      operation.action !==
        (operation.type === "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
          ? "CREATE"
          : "UPDATE") ||
      (operation.type === "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" &&
        (recipeRef !== operation.target.ref ||
          operation.parameters.recipeRef !== recipeRef ||
          operation.target.version !== operation.expectedVersion))
    )
      return;
    return {
      resourceScope: { kind: "ORGANIZATION", organizationRef },
      recipeRef,
    };
  }
  const createdRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_ROLE_IMAGE_RECIPE",
    "ROLE_IMAGE_RECIPE",
  );
  const updatedRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "UPDATE_ROLE_IMAGE_RECIPE",
    "ROLE_IMAGE_RECIPE",
  );
  const recipeRef =
    createdRef ||
    (updatedRef === operation?.target.ref ? updatedRef : undefined);
  return plan.projectRef && recipeRef
    ? { projectRef: plan.projectRef, recipeRef }
    : undefined;
}

export function systemAssistantImageOperationScope(
  operation: Pick<AssistantPlanOperation, "parameters" | "after">,
  organizationRef: string | undefined,
): boolean {
  const { parameters, after } = operation;
  return Boolean(
    organizationRef &&
    /^[A-Za-z0-9_-]{8,128}$/.test(organizationRef) &&
    parameters.scopeKind === "ORGANIZATION" &&
    after.scopeKind === "ORGANIZATION" &&
    parameters.organizationRef === organizationRef &&
    after.organizationRef === organizationRef &&
    typeof parameters.systemAssistantRef === "string" &&
    /^[A-Za-z0-9_-]{8,128}$/.test(parameters.systemAssistantRef) &&
    after.systemAssistantRef === parameters.systemAssistantRef &&
    !Object.hasOwn(parameters, "projectRef") &&
    !Object.hasOwn(after, "projectRef"),
  );
}

export function assistantEnvironmentDraftTarget(
  plan: AssistantPlan,
  operationRef: string,
  organizationRef?: string,
): { projectRef: string; draftRef: string } | undefined {
  const createdRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
    "RUNTIME_ENVIRONMENT_DRAFT",
  );
  const revisedRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
    "ENVIRONMENT",
  );
  const draftRef = createdRef || revisedRef;
  const operation = plan.operations.find((value) => value.ref === operationRef);
  const projectRef = operation
    ? assistantOperationProjectRef(plan, operation, organizationRef)
    : undefined;
  return projectRef && draftRef ? { projectRef, draftRef } : undefined;
}
export function assistantSystemEnvironmentDraftTarget(
  plan: AssistantPlan,
  operationRef: string,
):
  | { draftRef: string; environmentRef: string; assistantRef: string }
  | undefined {
  const draftRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "PREPARE_RUNTIME_ENVIRONMENT_REVISION",
    "ENVIRONMENT",
  );
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const assistantRef = operation?.parameters.systemAssistantRef;
  if (
    !draftRef ||
    (operation && hasAssistantProjectHelperLocator(operation)) ||
    typeof assistantRef !== "string" ||
    !/^[A-Za-z0-9_-]{8,128}$/.test(assistantRef) ||
    !operation?.target.ref
  )
    return;
  return { draftRef, environmentRef: operation.target.ref, assistantRef };
}

export function assistantCreatedProjectFileTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; artifactRef: string } | undefined {
  const artifactRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_PROJECT_FILE",
    "ARTIFACT",
  );
  return plan.projectRef && artifactRef
    ? { projectRef: plan.projectRef, artifactRef }
    : undefined;
}

export function assistantAgentEnvironmentBindingTarget(
  plan: AssistantPlan,
  operationRef: string,
  organizationRef?: string,
):
  | {
      projectRef: string;
      agentRef: string;
      environmentRef: string;
      versionRef: string;
    }
  | undefined {
  const agentRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "BIND_AGENT_RUNTIME_ENVIRONMENT",
    "AGENT",
  );
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const environmentRef = operation?.after.environmentRef;
  const versionRef = operation?.after.versionRef;
  const projectRef = operation
    ? assistantOperationProjectRef(plan, operation, organizationRef)
    : undefined;
  return projectRef &&
    agentRef &&
    operation?.target.ref === agentRef &&
    typeof environmentRef === "string" &&
    typeof versionRef === "string"
    ? { projectRef, agentRef, environmentRef, versionRef }
    : undefined;
}

export function assistantInstructionDraftTarget(
  plan: AssistantPlan,
  operationRef: string,
  organizationRef?: string,
): { projectRef: string; agentRef: string } | undefined {
  const agentRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_INSTRUCTION_DRAFT",
    "AGENT",
  );
  const operation = plan.operations.find((value) => value.ref === operationRef);
  const projectRef = operation
    ? assistantOperationProjectRef(plan, operation, organizationRef)
    : undefined;
  return projectRef && agentRef && operation?.target.ref === agentRef
    ? { projectRef, agentRef }
    : undefined;
}

export function assistantIntegrationConnectionTarget(
  plan: AssistantPlan,
  operationRef: string,
): { connectionRef: string } | undefined {
  const prepared = plan.operations.find((item) => item.ref === operationRef);
  const projectPreparedRef =
    prepared && projectAssistantConnectionPlanOwner(prepared)
      ? assistantAppliedResourceRef(
          plan,
          operationRef,
          "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION",
          "PROJECT_ASSISTANT",
        )
      : undefined;
  const createdRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_INTEGRATION_CONNECTION",
    "INTEGRATION_CONNECTION",
  );
  const updatedRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "UPDATE_INTEGRATION_CONNECTION",
    "INTEGRATION_CONNECTION",
  );
  const testedRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "TEST_INTEGRATION_CONNECTION",
    "INTEGRATION_CONNECTION",
  );
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const connectionRef =
    projectPreparedRef ||
    createdRef ||
    (updatedRef === operation?.target.ref ? updatedRef : undefined) ||
    (testedRef === operation?.target.ref ? testedRef : undefined);
  return connectionRef ? { connectionRef } : undefined;
}

export function assistantCreatedScheduleTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; scheduleRef: string } | undefined {
  const createdRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_SCHEDULE",
    "SCHEDULE",
  );
  const updatedRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "UPDATE_SCHEDULE",
    "SCHEDULE",
  );
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const scheduleRef =
    createdRef ||
    (updatedRef === operation?.target.ref ? updatedRef : undefined);
  return plan.projectRef && scheduleRef
    ? { projectRef: plan.projectRef, scheduleRef }
    : undefined;
}

export function assistantCreatedWorkflowTarget(
  plan: AssistantPlan,
  operationRef: string,
): { projectRef: string; workflowRef: string } | undefined {
  const createdRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "CREATE_WORKFLOW",
    "WORKFLOW",
  );
  const updatedRef = assistantAppliedResourceRef(
    plan,
    operationRef,
    "UPDATE_WORKFLOW",
    "WORKFLOW",
  );
  const operation = plan.operations.find((item) => item.ref === operationRef);
  const workflowRef =
    createdRef ||
    (updatedRef === operation?.target.ref ? updatedRef : undefined);
  return plan.projectRef && workflowRef
    ? { projectRef: plan.projectRef, workflowRef }
    : undefined;
}

export function assistantCreatedEntityTarget(
  plan: AssistantPlan,
  operationRef: string,
):
  | {
      kind: "PROJECT" | "AGENT" | "PROJECT_ASSISTANT";
      projectRef: string;
      resourceRef: string;
    }
  | undefined {
  const operation = plan.operations.find((item) => item.ref === operationRef);
  if (operation?.type === "CREATE_PROJECT_ASSISTANT") {
    const profileRef = assistantAppliedResourceRef(
      plan,
      operationRef,
      "CREATE_PROJECT_ASSISTANT",
      "PROJECT_ASSISTANT",
    );
    return plan.projectRef && profileRef
      ? {
          kind: "PROJECT_ASSISTANT",
          projectRef: plan.projectRef,
          resourceRef: profileRef,
        }
      : undefined;
  }
  if (
    operation?.type === "CREATE_PROJECT" ||
    operation?.type === "UPDATE_PROJECT"
  ) {
    const projectRef = assistantAppliedResourceRef(
      plan,
      operationRef,
      operation.type,
      "PROJECT",
    );
    return projectRef
      ? { kind: "PROJECT", projectRef, resourceRef: projectRef }
      : undefined;
  }
  if (
    operation?.type === "CREATE_AGENT" ||
    operation?.type === "UPDATE_AGENT"
  ) {
    const agentRef = assistantAppliedResourceRef(
      plan,
      operationRef,
      operation.type,
      "AGENT",
    );
    return plan.projectRef && agentRef
      ? { kind: "AGENT", projectRef: plan.projectRef, resourceRef: agentRef }
      : undefined;
  }
  return undefined;
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

export function assistantActiveUserTurn(
  conversation?: AssistantConversation,
  runs: Readonly<Record<string, Run>> = {},
  organizationRef?: string,
): AssistantTurn | undefined {
  if (conversation?.state !== "ACTIVE") return undefined;
  let running: AssistantTurn | undefined;
  let queued: AssistantTurn | undefined;
  for (const turn of conversation.turns) {
    if (turn.role !== "USER") continue;
    if (!["QUEUED", "RUNNING", "COMPLETED"].includes(turn.state)) continue;
    let run = turn.runRef ? runs[turn.runRef] : undefined;
    if (run) {
      const pin = run.assistantPin;
      try {
        assertRunOwner(run, organizationRef);
        if (
          run.ref !== turn.runRef ||
          run.target.type !== "SYSTEM_ASSISTANT" ||
          !pin ||
          pin.conversationRef !== conversation.ref ||
          pin.scope !== conversation.assistantScope ||
          pin.assistantRef !== conversation.assistantRef ||
          pin.projectRef !== conversation.projectRef ||
          pin.profileRef !== conversation.assistantProfileRef ||
          (turn.runVersion !== undefined && run.version < turn.runVersion)
        )
          run = undefined;
      } catch {
        run = undefined;
      }
    }
    // Terminal run закрывает ожидание даже при запаздывающей квитанции USER.
    // COMPLETED USER сам по себе подтверждает только сохранение сообщения.
    if (run && ["SUCCEEDED", "FAILED", "CANCELLED"].includes(run.state))
      continue;
    if (!run && turn.state === "COMPLETED") continue;
    const queuedState = run ? run.state === "QUEUED" : turn.state === "QUEUED";
    if (!queuedState && (!running || turn.sequence < running.sequence))
      running = turn;
    if (queuedState && (!queued || turn.sequence < queued.sequence))
      queued = turn;
  }
  return running ?? queued;
}

export function assistantAwaitingReply(
  conversation?: AssistantConversation,
  runs: Readonly<Record<string, Run>> = {},
  organizationRef?: string,
): boolean {
  return Boolean(assistantActiveUserTurn(conversation, runs, organizationRef));
}

export function assistantConversationStorageBlocker(
  conversation: AssistantConversation | undefined,
  runs: Readonly<Record<string, Run>>,
  organizationRef: string | undefined,
): "ERROR" | "PURGED" | undefined {
  if (conversation?.state !== "ACTIVE") return undefined;
  const latest = conversation.turns.reduce<AssistantTurn | undefined>(
    (current, turn) =>
      turn.role === "USER" && (!current || turn.sequence > current.sequence)
        ? turn
        : current,
    undefined,
  );
  // Более старый ход не доказывает состояние текущей сессии диалога.
  const run = latest?.runRef ? runs[latest.runRef] : undefined;
  const pin = run?.assistantPin;
  if (
    !run ||
    !latest ||
    run.ref !== latest.runRef ||
    run.target.type !== "SYSTEM_ASSISTANT" ||
    !pin ||
    pin.conversationRef !== conversation.ref ||
    pin.scope !== conversation.assistantScope ||
    pin.assistantRef !== conversation.assistantRef ||
    pin.projectRef !== conversation.projectRef ||
    pin.profileRef !== conversation.assistantProfileRef ||
    (latest.runVersion !== undefined && run.version < latest.runVersion)
  )
    return undefined;
  return runSessionStorageBlocker(run, organizationRef);
}

export interface EditablePlanOperation {
  value: AssistantPlanOperationInput;
  beforeText: string;
  parametersText: string;
  afterText: string;
}

export type FriendlyPlanOperationType =
  | "CREATE_PROJECT"
  | "CREATE_PROJECT_FILE"
  | "CREATE_PROJECT_FILE_REVISION"
  | "UPDATE_PROJECT"
  | "CREATE_AGENT"
  | "CREATE_PROJECT_ASSISTANT"
  | "UPDATE_AGENT"
  | "CREATE_INSTRUCTION_DRAFT"
  | "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS"
  | "CHANGE_CAPABILITY"
  | "CHANGE_INTEGRATION_GRANT"
  | "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT"
  | "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT"
  | "CREATE_WORKFLOW"
  | "UPDATE_WORKFLOW"
  | "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
  | "BIND_AGENT_RUNTIME_ENVIRONMENT"
  | "CREATE_SCHEDULE"
  | "UPDATE_SCHEDULE"
  | "CREATE_RUNTIME_ENVIRONMENT_DRAFT"
  | "CREATE_ROLE_IMAGE_RECIPE"
  | "UPDATE_ROLE_IMAGE_RECIPE"
  | "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
  | "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
  | "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION"
  | "CREATE_INTEGRATION_CONNECTION"
  | "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"
  | "UPDATE_INTEGRATION_CONNECTION"
  | "TEST_INTEGRATION_CONNECTION"
  | "PUBLISH_INTEGRATION_DEFINITION"
  | "ARCHIVE_AGENT"
  | "ARCHIVE_WORKFLOW"
  | "LAUNCH_RUN";

export function friendlyPlanOperationType(
  operation: EditablePlanOperation,
): FriendlyPlanOperationType | undefined {
  if (operation.value.type === "CREATE_PROJECT_FILE_REVISION") {
    try {
      return projectFileRevisionSource({
        ...operation.value,
        parameters: parseObject(operation.parametersText),
        before: parseObject(operation.beforeText),
        after: parseObject(operation.afterText),
      })
        ? operation.value.type
        : undefined;
    } catch {
      return undefined;
    }
  }
  if (
    operation.value.type === "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"
  ) {
    try {
      return projectAssistantConnectionPlanOwner({
        ...operation.value,
        parameters: parseObject(operation.parametersText),
        before: parseObject(operation.beforeText),
        after: parseObject(operation.afterText),
      })
        ? operation.value.type
        : undefined;
    } catch {
      return undefined;
    }
  }
  if (
    operation.value.type === "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" ||
    operation.value.type === "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT"
  )
    return operation.value.type;
  const operationType: FriendlyPlanOperationType = operation.value.type;
  let parameters: Record<string, unknown>;
  try {
    parameters = parseObject(operation.parametersText);
    parseObject(operation.beforeText);
    parseObject(operation.afterText);
  } catch {
    return undefined;
  }
  if (operation.value.type === "LAUNCH_RUN") {
    const targetType = parameters.targetType;
    const targetRef = parameters.targetRef;
    if (
      (targetType !== "AGENT" && targetType !== "WORKFLOW") ||
      typeof targetRef !== "string" ||
      !targetRef ||
      (operation.value.target.kind !== "EXECUTION" &&
        operation.value.target.kind !== targetType) ||
      (operation.value.target.ref !== undefined &&
        operation.value.target.ref !== targetRef)
    )
      return undefined;
    return operation.value.action === "EXECUTE" ? operationType : undefined;
  }
  const expectedKind =
    operation.value.type === "CREATE_PROJECT_ASSISTANT"
      ? "PROJECT_ASSISTANT"
      : operation.value.type === "CREATE_PROJECT_FILE"
        ? "ARTIFACT"
        : operation.value.type === "CREATE_ROLE_IMAGE_RECIPE" ||
            operation.value.type === "UPDATE_ROLE_IMAGE_RECIPE" ||
            operation.value.type ===
              "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" ||
            operation.value.type === "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
          ? "ROLE_IMAGE_RECIPE"
          : operation.value.type === "PUBLISH_INTEGRATION_DEFINITION"
            ? "INTEGRATION_DEFINITION"
            : operation.value.type === "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS"
              ? "SYSTEM_ASSISTANT"
              : operation.value.type === "CREATE_INTEGRATION_CONNECTION" ||
                  operation.value.type === "UPDATE_INTEGRATION_CONNECTION" ||
                  operation.value.type === "TEST_INTEGRATION_CONNECTION" ||
                  operation.value.type === "CHANGE_INTEGRATION_GRANT"
                ? "INTEGRATION_CONNECTION"
                : operation.value.type === "CREATE_WORKFLOW" ||
                    operation.value.type === "UPDATE_WORKFLOW" ||
                    operation.value.type === "ARCHIVE_WORKFLOW"
                  ? "WORKFLOW"
                  : operation.value.type ===
                      "PREPARE_RUNTIME_ENVIRONMENT_REVISION"
                    ? "ENVIRONMENT"
                    : operation.value.type === "CREATE_SCHEDULE" ||
                        operation.value.type === "UPDATE_SCHEDULE"
                      ? "SCHEDULE"
                      : operation.value.type.endsWith("PROJECT")
                        ? "PROJECT"
                        : operation.value.type ===
                            "CREATE_RUNTIME_ENVIRONMENT_DRAFT"
                          ? "RUNTIME_ENVIRONMENT_DRAFT"
                          : "AGENT";
  const expectedAction =
    operation.value.type === "CREATE_INSTRUCTION_DRAFT" ||
    operation.value.type === "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS"
      ? "UPDATE"
      : operation.value.type === "ARCHIVE_AGENT" ||
          operation.value.type === "ARCHIVE_WORKFLOW"
        ? "ARCHIVE"
        : operation.value.type.startsWith("CREATE_")
          ? "CREATE"
          : operation.value.type === "TEST_INTEGRATION_CONNECTION"
            ? "EXECUTE"
            : "UPDATE";
  if (
    operation.value.target.kind !== expectedKind ||
    operation.value.action !== expectedAction
  )
    return undefined;
  return operationType;
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
  if (isProjectFileOperation(operation.value.type)) {
    if (key === "content") {
      delete parameters.contentRef;
      delete parameters.digest;
      delete parameters.sizeBytes;
    }
    operation.parametersText = prettyJSON(parameters);
    return;
  }
  const nextAfter =
    operation.value.type === "CREATE_PROJECT_FILE" && key === "content"
      ? Object.fromEntries(
          Object.entries(after).filter(([candidate]) => candidate !== key),
        )
      : { ...after, [key]: value };
  operation.parametersText = prettyJSON(parameters);
  operation.afterText = prettyJSON(nextAfter);
  if (
    key === "name" &&
    operation.value.action === "CREATE" &&
    operation.value.type !==
      "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION" &&
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
  return operations.map((operation) => {
    const after = parseObject(operation.afterText);
    if (isProjectFileOperation(operation.value.type)) delete after.content;
    return {
      ...cloneOperation(operation.value),
      parameters: parseObject(operation.parametersText),
      before: parseObject(operation.beforeText),
      after,
    };
  });
}

export function honestEditedPlanSummaries(
  plan: AssistantPlan,
  auditSummary: string,
  operations: readonly AssistantPlanOperationInput[],
  labels: { plan: string; operation: string },
): {
  auditSummary: string;
  operations: AssistantPlanOperationInput[];
} {
  const revisions = operations.map((operation) => {
    const original = plan.operations.find((item) => item.ref === operation.ref);
    if (!original) return { operation, changed: false };
    const content = (item: AssistantPlanOperationInput) => {
      const after = cloneJSONRecord(item.after);
      if (isProjectFileOperation(item.type)) delete after.content;
      return JSON.stringify([
        item.type,
        item.action,
        item.title,
        item.target,
        item.expectedVersion,
        item.parameters,
        item.before,
        after,
      ]);
    };
    const contentChanged = content(operation) !== content(original);
    return {
      operation:
        contentChanged && operation.summary === original.summary
          ? { ...operation, summary: labels.operation }
          : operation,
      changed: contentChanged || operation.selected !== original.selected,
    };
  });
  return {
    auditSummary:
      revisions.some((item) => item.changed) &&
      auditSummary === plan.auditSummary
        ? labels.plan
        : auditSummary,
    operations: revisions.map((item) => item.operation),
  };
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

export function operationSupportingTitle(
  operation: AssistantPlanOperation,
): string | undefined {
  const title = operation.title.trim();
  const summary = operation.summary.trim();
  const titlePrefix = title.replace(/\s*(?:…|\.\.\.)$/, "").trim();
  return title &&
    title !== operationTargetLabel(operation.target) &&
    (!titlePrefix || !summary || !summary.startsWith(titlePrefix))
    ? title
    : undefined;
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

export function latestAssistantSnapshot(
  administration: SystemAssistant,
  live: SystemAssistant | undefined,
): SystemAssistant {
  if (!live || live.ref !== administration.ref) return administration;
  if (live.version !== administration.version)
    return live.version > administration.version ? live : administration;
  const administrationHeartbeat = Date.parse(
    administration.lastHeartbeatAt ?? "",
  );
  const liveHeartbeat = Date.parse(live.lastHeartbeatAt ?? "");
  return Number.isFinite(administrationHeartbeat) &&
    Number.isFinite(liveHeartbeat) &&
    liveHeartbeat < administrationHeartbeat
    ? administration
    : live;
}

export function assistantRequiresProviderAccount(
  assistant: SystemAssistant,
): boolean {
  return (
    !assistant.warmSessionRef &&
    !assistant.nextActions.includes("CREATE_CONVERSATION")
  );
}
