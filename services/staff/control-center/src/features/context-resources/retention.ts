import type { MemoryRecordRevision } from "@/shared/api/generated/openapi/types.gen";
export function memoryContentAvailable(
  revision: Pick<MemoryRecordRevision, "redacted" | "retentionUntil">,
  now: number,
): boolean {
  const expiry = Date.parse(revision.retentionUntil);
  return !revision.redacted && Number.isFinite(expiry) && expiry > now;
}
