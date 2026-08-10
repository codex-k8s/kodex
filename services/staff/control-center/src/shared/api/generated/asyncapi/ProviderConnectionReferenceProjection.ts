import {ProviderConnectionMaskedStatus} from './ProviderConnectionMaskedStatus';
interface ProviderConnectionReferenceProjection {
  stableKey: string;
  provider: string;
  referenceVersion: number;
  maskedLabel: string;
  maskedStatus: ProviderConnectionMaskedStatus;
  capabilities: string[];
  eligible: boolean;
  observedAt: string;
  observedUsage: number;
  observedLimit: number;
  observationExpiresAt: string;
}
export { ProviderConnectionReferenceProjection };