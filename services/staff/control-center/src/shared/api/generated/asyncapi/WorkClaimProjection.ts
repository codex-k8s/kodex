
interface WorkClaimProjection {
  processRunId: string;
  turnId: string;
  domains: string[];
  resourceKeys: string[];
  workloadId: string;
  sessionId: string;
  attempt: number;
  expiresAt: string;
  authorityGeneration: number;
}
export { WorkClaimProjection };