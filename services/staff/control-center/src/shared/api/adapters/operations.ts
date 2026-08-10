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
import { collectAllPages } from "@/shared/api/pagination";
import { unwrap } from "@/shared/api/problem";
import { executeMutation } from "@/shared/lib/identity";

type MutationHeaders = {
  "X-CSRF-Token": string;
  "Idempotency-Key": string;
  "If-Match": string;
};

export async function fetchRuns(): Promise<RunPage> {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listRuns({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.runs,
  );
  return { runs: values };
}

export async function fetchIncidents(): Promise<IncidentPage> {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listIncidents({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.incidents,
  );
  return { incidents: values };
}

export async function fetchBackups(): Promise<BackupPage> {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listBackups({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.backups,
  );
  return { backups: values };
}

export async function fetchBackup(backupId: string): Promise<Backup> {
  return (
    await unwrap(getBackup({ path: { backupId }, signal: requestSignal() }))
  ).data;
}

export async function restoreSessionBackup(
  backup: Backup,
): Promise<RestoreOperation> {
  const body = {
    sourceVersion: backup.sourceVersion,
    archiveSha256: backup.archiveSha256,
    provenanceSha256: backup.provenanceSha256,
    scope: backup.scope,
    scopeId: backup.scopeId,
  };
  return (
    await executeMutation(
      `backup:restore:${backup.backupId}`,
      body,
      backup.version,
      (headers) =>
        restoreBackup({
          body,
          path: { backupId: backup.backupId },
          headers: headers as MutationHeaders,
          signal: requestSignal(),
        }),
    )
  ).data;
}

export async function fetchRestoreOperations(
  backupId?: string,
): Promise<RestoreOperationPage> {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listRestoreOperations({
            query: {
              pageSize: 100,
              ...(backupId ? { backupId } : {}),
              ...(pageToken ? { pageToken } : {}),
            },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.operations,
  );
  return { operations: values };
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
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listAuditEvents({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.events,
  );
  return { events: values };
}

export async function fetchConfigurationChanges(): Promise<ConfigurationChangePage> {
  const { values } = await collectAllPages(
    async (pageToken) =>
      (
        await unwrap(
          listConfigurationChanges({
            query: { pageSize: 100, ...(pageToken ? { pageToken } : {}) },
            signal: requestSignal(),
          }),
        )
      ).data,
    (page) => page.changes,
  );
  return { changes: values };
}

export async function fetchDiagnostics(): Promise<Diagnostics> {
  return (await unwrap(getDiagnostics({ signal: requestSignal() }))).data;
}

export async function resolveGate(
  resource: Resource,
  body: ResolveOwnerGate,
): Promise<ResolveOwnerGateResult> {
  return (
    await executeMutation(
      `owner-gate:resolve:${resource.id}`,
      body,
      resource.version,
      (headers) =>
        resolveOwnerGate({
          body,
          path: { ownerGateId: resource.id },
          headers: headers as MutationHeaders,
          signal: requestSignal(),
        }),
    )
  ).data;
}
