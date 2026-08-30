import type {
  AssistantPlanOperation,
  AssistantPlanOperationInput,
  AssistantPlanTarget,
} from "@/shared/api/generated/openapi/types.gen";

export interface EditablePlanOperation {
  value: AssistantPlanOperationInput;
  beforeText: string;
  parametersText: string;
  afterText: string;
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
