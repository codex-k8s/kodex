import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface RepositoryWorkspaceProjection {
  repositoryStatus: string;
  workspaceMode: string;
  defaultBranch: string;
  credentialBindingStatus: string;
  ownership: ConfigurationOwnershipProjection;
}
export { RepositoryWorkspaceProjection };