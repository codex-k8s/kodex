import {
  getBackup,
  getDiagnostics,
  getRestoreOperation,
  listAuditEvents,
  listBackups,
  listConfigurationChanges,
  listIncidents,
  listRestoreOperations,
  listRuns,
  resolveOwnerGate,
  restoreBackup,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AuditPage,
  Backup,
  BackupPage,
  ConfigurationChangePage,
  Diagnostics,
  IncidentPage,
  ResolveOwnerGate,
  ResolveOwnerGateResult,
  Resource,
  RestoreOperation,
  RestoreOperationPage,
  RunPage,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";
import { mutationHeaders } from "@/shared/lib/identity";

type MutationHeaders = {
  "X-CSRF-Token": string;
  "Idempotency-Key": string;
  "If-Match": string;
};

export async function fetchRuns(): Promise<RunPage> {
  return (
    await unwrap(
      listRuns({ query: { pageSize: 100 }, signal: requestSignal() }),
    )
  ).data;
}

export async function fetchIncidents(): Promise<IncidentPage> {
  return (
    await unwrap(
      listIncidents({ query: { pageSize: 100 }, signal: requestSignal() }),
    )
  ).data;
}

export async function fetchBackups(): Promise<BackupPage> {
  return (
    await unwrap(
      listBackups({ query: { pageSize: 100 }, signal: requestSignal() }),
    )
  ).data;
}

export async function fetchBackup(backupId: string): Promise<Backup> {
  return (
    await unwrap(getBackup({ path: { backupId }, signal: requestSignal() }))
  ).data;
}

export async function restoreSessionBackup(
  backup: Backup,
): Promise<RestoreOperation> {
  return (
    await unwrap(
      restoreBackup({
        body: {
          sourceVersion: backup.sourceVersion,
          archiveSha256: backup.archiveSha256,
          provenanceSha256: backup.provenanceSha256,
          scope: backup.scope,
          scopeId: backup.scopeId,
        },
        path: { backupId: backup.backupId },
        headers: mutationHeaders(backup.version) as MutationHeaders,
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchRestoreOperations(
  backupId?: string,
): Promise<RestoreOperationPage> {
  return (
    await unwrap(
      listRestoreOperations({
        query: { pageSize: 100, ...(backupId ? { backupId } : {}) },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchRestoreOperation(
  restoreOperationId: string,
): Promise<RestoreOperation> {
  return (
    await unwrap(
      getRestoreOperation({
        path: { restoreOperationId },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchAudit(): Promise<AuditPage> {
  return (
    await unwrap(
      listAuditEvents({ query: { pageSize: 100 }, signal: requestSignal() }),
    )
  ).data;
}

export async function fetchConfigurationChanges(): Promise<ConfigurationChangePage> {
  return (
    await unwrap(
      listConfigurationChanges({
        query: { pageSize: 100 },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function fetchDiagnostics(): Promise<Diagnostics> {
  return (await unwrap(getDiagnostics({ signal: requestSignal() }))).data;
}

export async function resolveGate(
  resource: Resource,
  body: ResolveOwnerGate,
): Promise<ResolveOwnerGateResult> {
  return (
    await unwrap(
      resolveOwnerGate({
        body,
        path: { ownerGateId: resource.id },
        headers: mutationHeaders(resource.version) as MutationHeaders,
        signal: requestSignal(),
      }),
    )
  ).data;
}
