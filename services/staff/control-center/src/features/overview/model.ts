import type {
  Backup,
  Diagnostics,
  IncidentView,
  Resource,
  RunView,
} from "@/shared/api/generated/openapi/types.gen";

export interface OverviewDisplayValue {
  status: string;
  value: string;
}

export interface OverviewRun {
  runRef: string;
  displayName: string;
  state: string;
  updatedAt: string;
}

export interface OverviewResource {
  id: string;
  name: string;
  state: string;
  updatedAt: string;
  ownerGate?: { resolvable: boolean; deliveryState: string };
}

export interface OverviewIncident {
  incidentRef: string;
  kind: string;
  severity: string;
  occurredAt: string;
}

export interface OverviewBackup {
  backupId: string;
  scope: string;
  state: string;
  updatedAt: string;
  restorable: boolean;
}

export interface OverviewDiagnostics {
  schemaVersion: number;
  pendingOutboxEvents: number;
  terminalOutboxEvents: number;
  oldestPendingAgeSeconds: number;
  activeTurnLeases: number;
  queuedScheduleOccurrences: number;
  runtimePrincipalStatus: string;
  runtimePrincipalGeneration: number;
}

export const toOverviewRun = (value: RunView): OverviewRun => ({
  runRef: value.runRef,
  displayName: value.displayName,
  state: value.state,
  updatedAt: value.updatedAt,
});

export const toOverviewResource = (value: Resource): OverviewResource => ({
  id: value.id,
  name: value.name,
  state: value.state,
  updatedAt: value.updatedAt,
  ...(value.spec.ownerGate
    ? {
        ownerGate: {
          resolvable: value.spec.ownerGate.resolvable,
          deliveryState: value.spec.ownerGate.deliveryState,
        },
      }
    : {}),
});

export const toOverviewIncident = (value: IncidentView): OverviewIncident => ({
  incidentRef: value.incidentRef,
  kind: value.kind,
  severity: value.severity,
  occurredAt: value.occurredAt,
});

export const toOverviewBackup = (value: Backup): OverviewBackup => ({
  backupId: value.backupId,
  scope: value.scope,
  state: value.state,
  updatedAt: value.updatedAt,
  restorable: value.restorable,
});

export const toOverviewDiagnostics = (
  value: Diagnostics,
): OverviewDiagnostics => ({
  schemaVersion: value.schemaVersion,
  pendingOutboxEvents: value.pendingOutboxEvents,
  terminalOutboxEvents: value.terminalOutboxEvents,
  oldestPendingAgeSeconds: value.oldestPendingAgeSeconds,
  activeTurnLeases: value.activeTurnLeases,
  queuedScheduleOccurrences: value.queuedScheduleOccurrences,
  runtimePrincipalStatus: value.runtimePrincipalStatus,
  runtimePrincipalGeneration: value.runtimePrincipalGeneration,
});
