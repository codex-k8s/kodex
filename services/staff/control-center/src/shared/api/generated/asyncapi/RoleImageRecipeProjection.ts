import {RoleImageRecipeStatusInput} from './RoleImageRecipeStatusInput';
interface RoleImageRecipeProjection {
  /**
   * Безопасная read/status проекция без installation block и build secret references.
   */
  input: RoleImageRecipeStatusInput;
  generation: number;
  specSha256: string;
  policyRevision: number;
  policySha256: string;
  roleRuntimeContractRevision: number;
  roleRuntimeContractSha256: string;
}
export { RoleImageRecipeProjection };