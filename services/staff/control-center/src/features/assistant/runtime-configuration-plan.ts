import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";
import {
  accountSnapshotAvailable,
  type ModelSelection,
} from "@/features/providers/model-catalog";
import { orderedReasoningEfforts } from "@/features/providers/catalog-presentation";

export function assistantRuntimeReasoning(
  selection: ModelSelection | undefined,
  model: string,
  provider: string,
  accountRefs: readonly string[],
): { efforts: string[]; unsupported: boolean } | undefined {
  const refs = [...accountRefs].sort();
  if (
    !selection ||
    selection.model !== model ||
    selection.providerDefinitionKey !== provider ||
    !refs.length ||
    new Set(refs).size !== refs.length ||
    selection.accounts.length !== refs.length
  )
    return undefined;
  const snapshots = [...selection.accounts].sort((left, right) =>
    left.accountRef < right.accountRef
      ? -1
      : left.accountRef > right.accountRef
        ? 1
        : 0,
  );
  if (
    snapshots.some(
      (snapshot, index) =>
        snapshot.accountRef !== refs[index] ||
        snapshot.providerDefinitionKey !== provider ||
        snapshot.model?.id !== model ||
        snapshot.model.providerDefinitionKey !== provider ||
        !accountSnapshotAvailable(snapshot),
    )
  )
    return undefined;
  const capabilities = snapshots.map(
    (snapshot) => snapshot.model?.reasoningEfforts ?? [],
  );
  return {
    efforts: orderedReasoningEfforts(
      (capabilities[0] ?? []).filter((effort) =>
        capabilities.every((values) => values.includes(effort)),
      ),
    ),
    unsupported: capabilities.every((values) => values.length === 0),
  };
}

export function assistantRuntimePlanOwner(
  operation: AssistantPlanOperationInput,
  organizationRef: string | undefined,
): boolean {
  const { parameters, after, before, target } = operation;
  const exactKeys = [
    "agentRef",
    "assistantScope",
    "scopeKind",
    "organizationRef",
    "projectRef",
    "assistantProfileRef",
  ];
  if (
    !organizationRef ||
    !/^[A-Za-z0-9_-]{8,128}$/.test(organizationRef) ||
    operation.action !== "UPDATE" ||
    parameters.organizationRef !== organizationRef ||
    operation.type !== "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" ||
    target.kind !== "AGENT" ||
    target.ref !== parameters.agentRef ||
    !target.ref ||
    target.version !== operation.expectedVersion ||
    !Number.isSafeInteger(target.version) ||
    (target.version ?? 0) < 1 ||
    before.agentVersion !== target.version ||
    exactKeys.some(
      (key) =>
        parameters[key] !== after[key] || parameters[key] !== before[key],
    )
  )
    return false;
  if (parameters.assistantScope === "SYSTEM")
    return (
      parameters.scopeKind === "ORGANIZATION" &&
      parameters.projectRef === "" &&
      parameters.assistantProfileRef === ""
    );
  return (
    parameters.assistantScope === "PROJECT" &&
    parameters.scopeKind === "PROJECT" &&
    typeof parameters.projectRef === "string" &&
    /^[A-Za-z0-9_-]{8,128}$/.test(parameters.projectRef) &&
    typeof parameters.assistantProfileRef === "string" &&
    /^[A-Za-z0-9_-]{8,128}$/.test(parameters.assistantProfileRef)
  );
}
