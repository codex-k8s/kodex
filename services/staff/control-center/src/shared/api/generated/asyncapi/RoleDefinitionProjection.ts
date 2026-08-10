import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface RoleDefinitionProjection {
  stableKey: string;
  description: string;
  capabilities: string[];
  allowedTargetRoleDefinitionRefs: string[];
  roleImageRecipeRef?: string;
  roleImageRecipeVersion: number;
  roleImageRecipeSha256?: string;
  ownership: ConfigurationOwnershipProjection;
}
export { RoleDefinitionProjection };