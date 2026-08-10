import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface IntegrationProjection {
  definitionRef: string;
  definitionVersion: number;
  capabilities: string[];
  credentialBindingCount: number;
  endpointStatus: string;
  ownership: ConfigurationOwnershipProjection;
}
export { IntegrationProjection };