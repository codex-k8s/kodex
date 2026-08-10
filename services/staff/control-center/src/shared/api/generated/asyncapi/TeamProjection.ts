import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface TeamProjection {
  stableKey: string;
  providerBindingStatus: string;
  memberCount: number;
  roleCount: number;
  ownership: ConfigurationOwnershipProjection;
}
export { TeamProjection };