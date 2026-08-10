import {WorkspaceMattermostMappingState} from './WorkspaceMattermostMappingState';
interface WorkspaceMattermostMappingProjection {
  workspaceRef: string;
  workspaceVersion: number;
  mappingGeneration: number;
  state: WorkspaceMattermostMappingState;
  providerObservedAt: string;
  providerEffectVersion: number;
  providerEffectGeneration: number;
}
export { WorkspaceMattermostMappingProjection };