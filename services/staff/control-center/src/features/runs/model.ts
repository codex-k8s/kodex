import type {
  Resource,
  ResolveOwnerGateDecision,
  RunArtifactView,
  RunDetail,
  RunLineage,
  RunTimelineEntry,
  RunView,
} from "@/shared/api/generated/openapi/types.gen";

export type OwnerGateResolution = ResolveOwnerGateDecision;
export type RunAction = "CANCEL" | "RETRY";

export interface RunListItem {
  runRef: string;
  displayName: string;
  state: string;
  workspaceName: string;
  agentName: string;
  durationSeconds: number;
  updatedAt: string;
}

export interface OwnerGateListItem {
  id: string;
  name: string;
  state: string;
  version: number;
  expiresAt?: string;
  decision: string;
  resolvable: boolean;
  nextAction: string;
}

export interface RunDetailModel {
  run: {
    runRef: string;
    displayName: string;
    version: number;
    state: string;
    runtimeStatus: { value: string };
    attempt: number;
    trigger: { value: string };
    initiator: { value: string };
    agent: { value: string };
    role: { value: string };
    model: { value: string };
    provider: { value: string };
    nextActions: RunAction[];
  };
}

export interface RunTimelineModel {
  eventRef: string;
  display: string;
  outcome: string;
  occurredAt: string;
}

export interface RunLineageModel {
  nodes: Array<{
    nodeRef: string;
    parentRef?: string;
    displayName: string;
    state: string;
    kind: string;
    attempt: number;
  }>;
}

export interface RunArtifactModel {
  artifactRef: string;
  displayName: string;
  mediaType: string;
  sizeBytes: number;
  status: string;
  sha256: string;
}

export const toRunListItem = (value: RunView): RunListItem => ({
  runRef: value.runRef,
  displayName: value.displayName,
  state: value.state,
  workspaceName: value.workspace.value,
  agentName: value.agent.value,
  durationSeconds: value.durationSeconds,
  updatedAt: value.updatedAt,
});

export const toOwnerGateListItem = (value: Resource): OwnerGateListItem => ({
  id: value.id,
  name: value.name,
  state: value.state,
  version: value.version,
  ...(value.spec.ownerGate?.expiresAt
    ? { expiresAt: value.spec.ownerGate.expiresAt }
    : {}),
  decision: value.spec.ownerGate?.decision ?? value.state,
  resolvable: value.spec.ownerGate?.resolvable === true,
  nextAction: value.spec.ownerGate?.nextAction ?? "READ_TERMINAL",
});

const display = (value: { value: string }) => ({ value: value.value });

export const toRunDetailModel = (value: RunDetail): RunDetailModel => ({
  run: {
    runRef: value.run.runRef,
    displayName: value.run.displayName,
    version: value.run.version,
    state: value.run.state,
    runtimeStatus: display(value.run.runtimeStatus),
    attempt: value.run.attempt,
    trigger: display(value.run.trigger),
    initiator: display(value.run.initiator),
    agent: display(value.run.agent),
    role: display(value.run.role),
    model: display(value.run.model),
    provider: display(value.run.provider),
    nextActions: [...value.run.nextActions],
  },
});

export const toRunTimelineModel = (
  value: RunTimelineEntry,
): RunTimelineModel => ({
  eventRef: value.eventRef,
  display: value.display,
  outcome: value.outcome,
  occurredAt: value.occurredAt,
});

export const toRunLineageModel = (value: RunLineage): RunLineageModel => ({
  nodes: value.nodes.map((item) => ({
    nodeRef: item.nodeRef,
    ...(item.parentRef ? { parentRef: item.parentRef } : {}),
    displayName: item.displayName,
    state: item.state,
    kind: item.kind,
    attempt: item.attempt,
  })),
});

export const toRunArtifactModel = (
  value: RunArtifactView,
): RunArtifactModel => ({
  artifactRef: value.artifactRef,
  displayName: value.displayName,
  mediaType: value.mediaType,
  sizeBytes: value.sizeBytes,
  status: value.status,
  sha256: value.sha256,
});
