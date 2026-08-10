import {RoleImagePlatform} from './RoleImagePlatform';
import {RoleImagePackage} from './RoleImagePackage';
import {RoleImageTool} from './RoleImageTool';
/**
 * Безопасная read/status проекция без installation block и build secret references.
 */
interface RoleImageRecipeStatusInput {
  baseImageReference: string;
  baseImageDigest: string;
  sourceRef: string;
  sourceRevision: string;
  sourceSha256: string;
  contextRef: string;
  contextSha256: string;
  builderSha256: string;
  frontendSha256: string;
  platforms: RoleImagePlatform[];
  reservedPackages: RoleImagePackage[];
  tools: RoleImageTool[];
  toolchainSha256: string;
}
export { RoleImageRecipeStatusInput };