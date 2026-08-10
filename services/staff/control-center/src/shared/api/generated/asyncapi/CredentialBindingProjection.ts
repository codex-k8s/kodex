import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface CredentialBindingProjection {
  purpose: string;
  revision: number;
  expiresAt?: string;
  providerEligible: boolean;
  providerCapabilities: string[];
  providerObservationRevision: number;
  providerObservedAt?: string;
  contentSha256: string;
  ownership: ConfigurationOwnershipProjection;
  bindingStatus: string;
}
export { CredentialBindingProjection };