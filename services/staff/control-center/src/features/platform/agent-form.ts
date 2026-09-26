export interface AgentDraft {
  readonly name: string;
  readonly purpose: string;
  readonly roleDescription: string;
  readonly initialInstructions: string;
  readonly runtimeRef: string;
}

export function isAgentDraftComplete(input: AgentDraft): boolean {
  return (
    input.name.trim().length > 0 &&
    input.name.length <= 120 &&
    input.purpose.trim().length > 0 &&
    input.purpose.length <= 1000 &&
    input.roleDescription.trim().length > 0 &&
    input.roleDescription.length <= 1000 &&
    input.initialInstructions.trim().length >= 20 &&
    input.initialInstructions.length <= 65536 &&
    input.runtimeRef.trim().length > 0
  );
}

export function resolveAgentRuntimeRef(
  current: string,
  available: readonly string[],
): string {
  if (available.includes(current)) return current;
  return available[0] ?? "";
}
