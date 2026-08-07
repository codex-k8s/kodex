
interface ProcessRunProjection {
  parentProcessRunId?: string;
  playbookRef: string;
  policyRevision: number;
  rootTriggerRef: string;
  rootSessionId: string;
  rootTurnId: string;
  rootAttempt: number;
  immutableInputSha256: string;
  runtimeRevisionId: string;
  currentSessionId?: string;
  currentTurnId?: string;
  currentAttempt?: number;
}
export { ProcessRunProjection };