import {ProviderPoolPolicy} from './ProviderPoolPolicy';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface ProviderPoolResourceProjection {
  stableKey: string;
  policy: ProviderPoolPolicy;
  policyRevision: number;
  observationMaxAgeSeconds: number;
  eligibleMembers: number;
  totalMembers: number;
  eligibilitySnapshotSha256: string;
  ownership: ConfigurationOwnershipProjection;
}
export { ProviderPoolResourceProjection };