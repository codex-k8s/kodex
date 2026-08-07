import {ProviderPoolProjection} from './ProviderPoolProjection';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface RoleProjection {
  stableKey: string;
  capabilities: string[];
  allowedTargetRoleIds: string[];
  promptProfileId: string;
  roleImageRecipeId: string;
  providerCredentialBindingIds: string[];
  repositoryWorkspaceIds: string[];
  integrationIds: string[];
  providerAccountPool: ProviderPoolProjection;
  ownership: ConfigurationOwnershipProjection;
}
export { RoleProjection };