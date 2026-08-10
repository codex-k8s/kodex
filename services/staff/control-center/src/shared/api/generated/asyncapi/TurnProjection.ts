
interface TurnProjection {
  sessionId: string;
  sequence: number;
  sourceRef: string;
  runtimeRevisionId: string;
  processRunId?: string;
  attempt: number;
  resultArtifactId?: string;
  effectiveInputSha256: string;
}
export { TurnProjection };