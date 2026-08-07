import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface TeamProjection {
  stableKey: string;
  externalTeamRef: string;
  memberActorIds: string[];
  roleIds: string[];
  ownership: ConfigurationOwnershipProjection;
}
export { TeamProjection };