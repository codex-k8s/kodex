import type {
  ConfigurationChange,
  ConfigurationDiff,
  ConfigurationSourceDetail,
  Resource,
  ResourceHistoryEntry,
} from "@/shared/api/generated/openapi/types.gen";

export type ConfigurationSourceKind =
  | "ROLE_DEFINITION"
  | "AGENT"
  | "INSTRUCTION_SET"
  | "PROVIDER_POOL";

export interface ConfigurationInstructionOption {
  ref: string;
  displayName: string;
}

export interface ConfigurationChangeModel {
  ref: string;
  action: string;
  resourceRef: string;
  resourceKind: string;
  resourceVersion: number;
  outcome: string;
  policyRevision: number;
  occurredAt: string;
}

export interface ConfigurationDiffModel {
  changes: Array<{
    kind: string;
    path: string;
    display: "TEXT" | "REDACTED";
    before: string;
    after: string;
  }>;
}

export interface ConfigurationSourceModel {
  resourceRef: string;
  displayName: string;
  version: number;
  managedBy: "ui" | "git";
  source: string;
  sourceRevision: number;
  sourceSha256?: string;
  drift: "NOT_APPLICABLE" | "IN_SYNC" | "DRIFTED" | "UNKNOWN";
}

export const toConfigurationInstructionOption = (
  value: Resource,
): ConfigurationInstructionOption => ({
  ref: value.id,
  displayName: value.name,
});

export const toConfigurationHistoryVersion = (
  value: ResourceHistoryEntry,
): number => value.resource.version;

export const toConfigurationChangeModel = (
  value: ConfigurationChange,
): ConfigurationChangeModel => ({
  ref: value.id,
  action: value.action,
  resourceRef: value.resourceId,
  resourceKind: value.resourceKind,
  resourceVersion: value.resourceVersion,
  outcome: value.outcome,
  policyRevision: value.policyRevision,
  occurredAt: value.occurredAt,
});

export const toConfigurationDiffModel = (
  value: ConfigurationDiff,
): ConfigurationDiffModel => ({
  changes: value.changes.map((item) => ({ ...item })),
});

export const toConfigurationSourceModel = (
  value: ConfigurationSourceDetail,
): ConfigurationSourceModel => ({
  resourceRef: value.resourceRef,
  displayName: value.displayName,
  version: value.version,
  managedBy: value.managedBy,
  source: value.source,
  sourceRevision: value.sourceRevision,
  ...(value.sourceSha256 ? { sourceSha256: value.sourceSha256 } : {}),
  drift: value.drift,
});
