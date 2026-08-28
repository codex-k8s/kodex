import type {
  AssistantPlanOperation,
  AssistantPlanOperationInput,
} from "@/shared/api/generated/openapi/types.gen";

export interface EditablePlanOperation {
  value: AssistantPlanOperationInput;
  parametersText: string;
  afterText: string;
}

function prettyJSON(value: Record<string, unknown>): string {
  return JSON.stringify(value, null, 2);
}

export function editableOperations(
  operations: readonly AssistantPlanOperation[],
): EditablePlanOperation[] {
  return operations.map((operation) => ({
    value: structuredClone(operation),
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
    ...structuredClone(operation.value),
    parameters: parseObject(operation.parametersText),
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
