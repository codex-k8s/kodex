import type {
  IncidentHistoryEntry,
  IncidentView,
} from "@/shared/api/generated/openapi/types.gen";

export type IncidentAction = "ACKNOWLEDGE" | "RETRY" | "RELEASE" | "CLOSE";

export interface IncidentListItem {
  incidentRef: string;
  kind: string;
  severity: string;
  state: string;
  workspace: {
    status: string;
    value: string;
  };
  impact: string;
  occurredAt: string;
}

export interface IncidentDetailModel extends IncidentListItem {
  version: number;
  run: { status: string; value: string };
  diagnosticSummary: string;
  runbookUrl: string;
  safeCorrelation: string;
  executionFence: number;
  nextActions: IncidentAction[];
}

export interface IncidentHistoryModel {
  version: number;
  state: string;
  action: IncidentAction;
  reasonCode: string;
  occurredAt: string;
  executionFence: number;
  nextActions: IncidentAction[];
}

export const toIncidentListItem = (value: IncidentView): IncidentListItem => ({
  incidentRef: value.incidentRef,
  kind: value.kind,
  severity: value.severity,
  state: value.state,
  workspace: {
    status: value.workspace.status,
    value: value.workspace.value,
  },
  impact: value.impact,
  occurredAt: value.occurredAt,
});

export const toIncidentDetailModel = (
  value: IncidentView,
): IncidentDetailModel => ({
  ...toIncidentListItem(value),
  version: value.version,
  run: { status: value.run.status, value: value.run.value },
  diagnosticSummary: value.diagnosticSummary,
  runbookUrl: value.runbookUrl,
  safeCorrelation: value.safeCorrelation,
  executionFence: value.executionFence,
  nextActions: [...value.nextActions],
});

export const toIncidentHistoryModel = (
  value: IncidentHistoryEntry,
): IncidentHistoryModel => ({
  version: value.version,
  state: value.state,
  action: value.action,
  reasonCode: value.reasonCode,
  occurredAt: value.occurredAt,
  executionFence: value.executionFence,
  nextActions: [...value.nextActions],
});
