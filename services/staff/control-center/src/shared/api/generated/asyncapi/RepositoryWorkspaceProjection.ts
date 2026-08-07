import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface RepositoryWorkspaceProjection {
  repositoryRef: string;
  workspaceMode: string;
  defaultBranch: string;
  credentialBindingId?: string;
  ownership: ConfigurationOwnershipProjection;
}
export { RepositoryWorkspaceProjection };