import type { Run, RunNode } from "@/shared/api/generated/openapi/types.gen";
import { requireRuntimeOrganizationRef } from "@/features/runtime/resource-scope";

const refPattern = /^[A-Za-z0-9_-]{8,128}$/;
function validRef(ref: unknown): ref is string {
  return typeof ref === "string" && refPattern.test(ref);
}

export function assertRunOwner(
  run: Run,
  organizationRef: string | undefined,
): void {
  const assistantSource = run.source === "SYSTEM_ASSISTANT";
  const assistantTarget = run.target.type === "SYSTEM_ASSISTANT";
  if (!assistantTarget) {
    if (run.assistantPin !== undefined || !validRef(run.projectRef))
      throw new Error("Regular run owner identity is invalid");
    return;
  }
  if (!assistantSource)
    throw new Error("Assistant run source identity is invalid");
  const pin = run.assistantPin;
  const owner = requireRuntimeOrganizationRef(organizationRef);
  if (
    !pin ||
    Object.keys(pin).some(
      (key) =>
        ![
          "scope",
          "organizationRef",
          "conversationRef",
          "assistantRef",
          "projectRef",
          "profileRef",
        ].includes(key),
    ) ||
    pin.organizationRef !== owner ||
    !validRef(pin.conversationRef) ||
    !validRef(pin.assistantRef) ||
    pin.assistantRef !== run.target.ref ||
    (pin.projectRef !== undefined && !validRef(pin.projectRef)) ||
    pin.projectRef !== run.projectRef
  )
    throw new Error("Assistant run owner pin is invalid");
  const scope: unknown = pin.scope;
  if (scope === "SYSTEM") {
    if (pin.profileRef !== undefined)
      throw new Error("System assistant run profile is forbidden");
  } else if (scope === "PROJECT") {
    if (!validRef(pin.projectRef) || !validRef(pin.profileRef))
      throw new Error("Project assistant run profile is invalid");
  } else throw new Error("Assistant run scope is invalid");
}

export function sameAssistantRunPin(previous: Run, next: Run): boolean {
  const pin = previous.assistantPin,
    returned = next.assistantPin;
  if (!pin || !returned) return pin === returned;
  return [
    "scope",
    "organizationRef",
    "conversationRef",
    "assistantRef",
    "projectRef",
    "profileRef",
  ].every(
    (key) =>
      pin[key as keyof typeof pin] === returned[key as keyof typeof returned],
  );
}

export function assertAssistantRetryIdentity(previous: Run, next: Run): void {
  const pin = previous.assistantPin,
    returned = next.assistantPin;
  if (previous.target.type !== "SYSTEM_ASSISTANT") return;
  if (
    !pin ||
    !returned ||
    next.target.type !== "SYSTEM_ASSISTANT" ||
    next.sessionRef !== previous.sessionRef ||
    next.ref === previous.ref ||
    next.attempt !== previous.attempt + 1 ||
    next.retryOfRunRef !== previous.ref ||
    !sameAssistantRunPin(previous, next)
  )
    throw new Error("Assistant retry owner pin mismatch");
}

export function assistantRunNodeRefs(
  nodes: readonly RunNode[],
  runs: Readonly<Record<string, Run>>,
  organizationRef: string | undefined,
): Set<string> {
  const refs = new Set<string>();
  for (const node of nodes) {
    const run = runs[node.runRef];
    if (
      node.type !== "AGENT_EXECUTION" ||
      !run ||
      run.ref !== node.runRef ||
      run.target.type !== "SYSTEM_ASSISTANT" ||
      (node.agentRef !== undefined &&
        node.agentRef !== run.assistantPin?.assistantRef)
    )
      continue;
    try {
      assertRunOwner(run, organizationRef);
      refs.add(node.ref);
    } catch {
      // Недоказанный owner pin не меняет подпись обычного сотрудника.
    }
  }
  return refs;
}

export function runNodeExecutionLabels(
  nodes: readonly RunNode[],
  runs: Readonly<Record<string, Run>>,
  organizationRef: string | undefined,
): Record<string, "ASSISTANT" | "EMPLOYEE" | "SESSION"> {
  const assistants = assistantRunNodeRefs(nodes, runs, organizationRef);
  const labels: Record<string, "ASSISTANT" | "EMPLOYEE" | "SESSION"> = {};
  for (const node of nodes) {
    if (node.type !== "AGENT_EXECUTION") continue;
    labels[node.ref] = "SESSION";
    if (assistants.has(node.ref)) {
      labels[node.ref] = "ASSISTANT";
      continue;
    }
    const run = runs[node.runRef];
    if (
      !run ||
      run.ref !== node.runRef ||
      run.target.type !== "AGENT" ||
      (node.agentRef !== undefined && node.agentRef !== run.target.ref)
    )
      continue;
    try {
      assertRunOwner(run, organizationRef);
      labels[node.ref] = "EMPLOYEE";
    } catch {
      // Неизвестная принадлежность сохраняет нейтральную подпись сессии.
    }
  }
  return labels;
}

export function runNodePresentationKey(
  label: "ASSISTANT" | "EMPLOYEE" | "SESSION" | undefined,
  type: RunNode["type"],
): string {
  return label === "ASSISTANT"
    ? "runs.assistantNode"
    : label === "SESSION"
      ? "runs.sessionNode"
      : `runs.nodeTypes.${type}`;
}
