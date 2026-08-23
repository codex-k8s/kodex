export interface AgentDraft {
  readonly name: string;
  readonly purpose: string;
  readonly roleDescription: string;
  readonly initialInstructions: string;
  readonly runtimeRef: string;
}

export function isAgentDraftComplete(input: AgentDraft): boolean {
  return [
    input.name,
    input.purpose,
    input.roleDescription,
    input.initialInstructions,
    input.runtimeRef,
  ].every((value) => value.trim().length > 0);
}
