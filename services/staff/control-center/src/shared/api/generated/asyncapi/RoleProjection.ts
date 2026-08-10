import {ProviderPoolProjection} from './ProviderPoolProjection';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface RoleProjection {
  stableKey: string;
  capabilities: string[];
  allowedTargetRoleCount: number;
  promptProfileStatus: string;
  roleImageRecipeStatus: string;
  providerCredentialBindingCount: number;
  repositoryWorkspaceCount: number;
  integrationCount: number;
  providerAccountPool: ProviderPoolProjection;
  ownership: ConfigurationOwnershipProjection;
}
export { RoleProjection };