import type { ProjectPurgeRecovery } from "@/shared/api/generated/openapi/types.gen";

export function purgeRecoveryBinding(value: ProjectPurgeRecovery): string {
  return JSON.stringify([
    value.projectRef,
    value.version,
    value.snapshotDigest,
  ]);
}

export function canResolvePurgeRecovery(value?: ProjectPurgeRecovery): boolean {
  return Boolean(
    value?.canResolve &&
    value.state === "WAITING_OWNER" &&
    Number.isSafeInteger(value.version) &&
    value.version > 0 &&
    /^[a-f0-9]{64}$/.test(value.snapshotDigest),
  );
}

export function purgeRecoveryStatusKey(value: ProjectPurgeRecovery): string {
  if (value.cleanupPendingCount > 0 && !value.canResolve)
    return "projects.recovery.cleanupPending";
  return `projects.recovery.state.${value.state}`;
}
