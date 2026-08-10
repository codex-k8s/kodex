import type {
  ConfigurationDiff,
  ConfigurationSourceDetail,
  InstructionSetComparison,
  Resource,
  ResourceHistoryEntry,
} from "@/shared/api/generated/openapi/types.gen";

export interface InstructionOwnershipModel {
  managedBy: "ui" | "git";
  source: string;
  revision: number;
  drift: string;
}

export interface InstructionSetModel {
  id: string;
  name: string;
  state: string;
  version: number;
  stableKey: string;
  locale: string;
  content: string;
  currentVersion: number;
  versionState: string;
  validationSucceeded: boolean;
  validationProblems: Array<{
    code: string;
    field: string;
    line: number;
    column: number;
    message: string;
  }>;
  ownership: InstructionOwnershipModel;
}

export interface InstructionHistoryModel {
  resourceId: string;
  resourceVersion: number;
  action: string;
  occurredAt: string;
  snapshotSha256: string;
}

export interface InstructionComparisonModel {
  contentEqual: boolean;
  comparisonSha256: string;
  leftVersion: number;
  rightVersion: number;
}

export interface InstructionConfigurationDiffModel {
  leftVersion: number;
  rightVersion: number;
  changes: Array<{
    kind: string;
    path: string;
    display: string;
    before: string;
    after: string;
  }>;
  truncated: boolean;
  nextPageToken: string | null;
}

export interface InstructionConfigurationSourceModel {
  resourceRef: string;
  displayName: string;
  version: number;
  managedBy: "ui" | "git";
  source: string;
  sourceRevision: number;
  drift: string;
  updatedAt: string;
}

const ownership = (value: Resource): InstructionOwnershipModel => {
  const source = value.spec.instructionSet?.ownership;
  return {
    managedBy: source?.managedBy ?? "ui",
    source: source?.source ?? "",
    revision: source?.revision ?? 0,
    drift: source?.drift ?? "NOT_APPLICABLE",
  };
};

export const toInstructionSetModel = (value: Resource): InstructionSetModel => {
  const projection = value.spec.instructionSet;
  return {
    id: value.id,
    name: value.name,
    state: value.state,
    version: value.version,
    stableKey: projection?.stableKey ?? "",
    locale: projection?.locale ?? "",
    content: projection?.content ?? "",
    currentVersion: projection?.currentVersion ?? value.version,
    versionState: projection?.versionState ?? value.state,
    validationSucceeded: projection?.validationSucceeded ?? false,
    validationProblems: (projection?.validationProblems ?? []).map((item) => ({
      code: item.code,
      field: item.field,
      line: item.line,
      column: item.column,
      message: item.message,
    })),
    ownership: ownership(value),
  };
};

export const toInstructionHistoryModel = (
  value: ResourceHistoryEntry,
): InstructionHistoryModel => ({
  resourceId: value.resource.id,
  resourceVersion: value.resource.version,
  action: value.action,
  occurredAt: value.occurredAt,
  snapshotSha256: value.snapshotSha256,
});

export const toInstructionComparisonModel = (
  value: InstructionSetComparison,
): InstructionComparisonModel => ({
  contentEqual: value.contentEqual,
  comparisonSha256: value.comparisonSha256,
  leftVersion: value.left.resource.version,
  rightVersion: value.right.resource.version,
});

export const toInstructionConfigurationDiffModel = (
  value: ConfigurationDiff,
): InstructionConfigurationDiffModel => ({
  leftVersion: value.left.version,
  rightVersion: value.right.version,
  changes: value.changes.map((item) => ({
    kind: item.kind,
    path: item.path,
    display: item.display,
    before: item.before,
    after: item.after,
  })),
  truncated: value.truncated,
  nextPageToken: value.nextPageToken ?? null,
});

export const toInstructionConfigurationSourceModel = (
  value: ConfigurationSourceDetail,
): InstructionConfigurationSourceModel => ({
  resourceRef: value.resourceRef,
  displayName: value.displayName,
  version: value.version,
  managedBy: value.managedBy,
  source: value.source,
  sourceRevision: value.sourceRevision,
  drift: value.drift,
  updatedAt: value.updatedAt,
});
