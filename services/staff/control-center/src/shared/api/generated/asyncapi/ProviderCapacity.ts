
interface ProviderCapacity {
  usage: number;
  limit: number;
  revision: number;
  observedAt: string;
  windowDurationSeconds: number;
  resetsAt?: string;
  expiresAt: string;
  digestSha256: string;
}
export { ProviderCapacity };