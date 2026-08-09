import {ProviderConnectionState} from './ProviderConnectionState';
import {ProviderCapacity} from './ProviderCapacity';
interface ProviderConnectionProjection {
  connectionRef: string;
  stableKey: string;
  providerRef: string;
  displayName: string;
  version: number;
  generation: number;
  state: ProviderConnectionState;
  maskedLabel: string;
  maskedAccount: string;
  capabilities: string[];
  capabilityDigestSha256: string;
  observationDigestSha256: string;
  observedAt: string;
  updatedAt: string;
  activeCredentialGeneration: number;
  capacity?: ProviderCapacity;
}
export { ProviderConnectionProjection };