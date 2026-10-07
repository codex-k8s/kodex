import type { AssistantPlanOperationInput } from "@/shared/api/generated/openapi/types.gen";

const ownerKeys = [
  "projectAssistantRef",
  "assistantScope",
  "scopeKind",
  "organizationRef",
  "projectRef",
  "assistantProfileRef",
  "agentVersion",
  "profileVersion",
  "definitionVersion",
  "definitionDigest",
] as const;
const editableKeys = ["definitionKey", "name", "publicConfiguration"] as const;
const opaque = (value: unknown) =>
  typeof value === "string" && /^[A-Za-z0-9_-]{8,96}$/.test(value);
const positive = (value: unknown) =>
  Number.isSafeInteger(value) && (value as number) > 0;
const equal = (a: unknown, b: unknown) =>
  JSON.stringify(a) === JSON.stringify(b);

// ORGANIZATION здесь назначено владельцем плана, а не выведено из отсутствия projectRef.
export function projectAssistantConnectionPlanOwner(
  operation: AssistantPlanOperationInput,
  organizationRef?: string,
): boolean {
  const { parameters, before, after, target } = operation;
  const parameterKeys = new Set<string>([...ownerKeys, ...editableKeys]);
  const beforeKeys = new Set<string>([...ownerKeys, "assistantName"]);
  return (
    operation.type === "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION" &&
    operation.action === "CREATE" &&
    parameters.assistantScope === "PROJECT" &&
    parameters.scopeKind === "ORGANIZATION" &&
    opaque(parameters.organizationRef) &&
    (!organizationRef || parameters.organizationRef === organizationRef) &&
    opaque(parameters.projectRef) &&
    opaque(parameters.projectAssistantRef) &&
    typeof parameters.assistantProfileRef === "string" &&
    /^asstp_[A-Za-z0-9_-]{8,90}$/.test(parameters.assistantProfileRef) &&
    positive(parameters.agentVersion) &&
    positive(parameters.profileVersion) &&
    typeof parameters.definitionVersion === "string" &&
    parameters.definitionVersion.length > 0 &&
    typeof parameters.definitionDigest === "string" &&
    /^[a-f0-9]{64}$/.test(parameters.definitionDigest) &&
    target.kind === "PROJECT_ASSISTANT" &&
    target.ref === parameters.projectAssistantRef &&
    operation.expectedVersion === parameters.agentVersion &&
    target.version === operation.expectedVersion &&
    typeof parameters.definitionKey === "string" &&
    parameters.definitionKey.length > 0 &&
    typeof parameters.name === "string" &&
    parameters.name.trim().length > 0 &&
    parameters.name.length <= 160 &&
    typeof before.assistantName === "string" &&
    before.assistantName.length > 0 &&
    target.name === before.assistantName &&
    typeof parameters.publicConfiguration === "object" &&
    parameters.publicConfiguration !== null &&
    !Array.isArray(parameters.publicConfiguration) &&
    Object.keys(parameters.publicConfiguration).length <= 100 &&
    Object.values(parameters.publicConfiguration).every(
      (value) => typeof value === "string" && value.length <= 4096,
    ) &&
    Object.keys(parameters).every((key) => parameterKeys.has(key)) &&
    Object.keys(after).every((key) => parameterKeys.has(key)) &&
    Object.keys(before).every((key) => beforeKeys.has(key)) &&
    ownerKeys.every(
      (key) =>
        equal(parameters[key], before[key]) &&
        equal(parameters[key], after[key]),
    ) &&
    editableKeys.every((key) => equal(parameters[key], after[key]))
  );
}
