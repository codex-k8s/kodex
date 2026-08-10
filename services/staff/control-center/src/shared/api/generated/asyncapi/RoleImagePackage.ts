import {RoleImagePackageManager} from './RoleImagePackageManager';
interface RoleImagePackage {
  manager: RoleImagePackageManager;
  reservedName: string;
  version: string;
  digest: string;
  sourceRef: string;
}
export { RoleImagePackage };