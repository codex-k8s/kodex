import type {
  MattermostMappingOperation,
  MattermostTeam,
  MattermostTeamBinding,
} from "@/shared/api/generated/openapi/types.gen";

export interface WorkspaceTeamModel {
  selector: string;
  displayName: string;
  slug: string;
  status: string;
  observedAt: string;
}

export interface WorkspaceTeamBindingModel {
  mappingVersion: number;
  mappingGeneration: number;
  state: string;
  team: WorkspaceTeamModel;
  providerEffectVersion: number;
}

export interface WorkspaceTeamOperationModel {
  operationRef: string;
  action: string;
  state: string;
  updatedAt: string;
}

export const toWorkspaceTeamModel = (
  value: MattermostTeam,
): WorkspaceTeamModel => ({
  selector: value.selector,
  displayName: value.displayName,
  slug: value.slug,
  status: value.status,
  observedAt: value.observedAt,
});

export const toWorkspaceTeamBindingModel = (
  value: MattermostTeamBinding,
): WorkspaceTeamBindingModel => ({
  mappingVersion: value.mappingVersion,
  mappingGeneration: value.mappingGeneration,
  state: value.state,
  team: toWorkspaceTeamModel(value.team),
  providerEffectVersion: value.providerEffectVersion,
});

export const toWorkspaceTeamOperationModel = (
  value: MattermostMappingOperation,
): WorkspaceTeamOperationModel => ({
  operationRef: value.operationRef,
  action: value.action,
  state: value.state,
  updatedAt: value.updatedAt,
});
