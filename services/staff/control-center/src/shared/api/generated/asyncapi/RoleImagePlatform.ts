import {RoleImagePlatformOs} from './RoleImagePlatformOs';
import {RoleImagePlatformArchitecture} from './RoleImagePlatformArchitecture';
interface RoleImagePlatform {
  os: RoleImagePlatformOs;
  architecture: RoleImagePlatformArchitecture;
  variant?: string;
}
export { RoleImagePlatform };