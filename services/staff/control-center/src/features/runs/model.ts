import type {
  Resource,
  ResolveOwnerGateDecision,
  RunView,
} from "@/shared/api/generated/openapi/types.gen";

export type OwnerGateResolution = ResolveOwnerGateDecision;

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
