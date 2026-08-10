import type {
  Diagnostics,
  HealthObservation,
} from "@/shared/api/generated/openapi/types.gen";

export interface HealthObservationModel {
  component: string;
  status: string;
  value: number;
  source: string;
  observedAt: string;
}

export interface DiagnosticsModel {
  schemaVersion: number;
  pendingOutboxEvents: number;
  terminalOutboxEvents: number;
  oldestPendingAgeSeconds: number;
  activeTurnLeases: number;
  queuedScheduleOccurrences: number;
  runtimePrincipalStatus: string;
  runtimePrincipalGeneration: number;
}

export const toHealthObservationModel = (
  value: HealthObservation,
): HealthObservationModel => ({
  component: value.component,
  status: value.status,
  value: value.value,
  source: value.source,
  observedAt: value.observedAt,
});

export const toDiagnosticsModel = (value: Diagnostics): DiagnosticsModel => ({
  schemaVersion: value.schemaVersion,
  pendingOutboxEvents: value.pendingOutboxEvents,
  terminalOutboxEvents: value.terminalOutboxEvents,
  oldestPendingAgeSeconds: value.oldestPendingAgeSeconds,
  activeTurnLeases: value.activeTurnLeases,
  queuedScheduleOccurrences: value.queuedScheduleOccurrences,
  runtimePrincipalStatus: value.runtimePrincipalStatus,
  runtimePrincipalGeneration: value.runtimePrincipalGeneration,
});
