import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface AgentProjection {
  stableKey: string;
  roleDefinitionRef: string;
  instructionSetRef: string;
  providerPoolRef: string;
  runtimeProfileRef?: string;
  capabilities: string[];
  botUsername?: string;
  botMaskedStatus?: string;
  enabled: boolean;
  ownership: ConfigurationOwnershipProjection;
}
export { AgentProjection };