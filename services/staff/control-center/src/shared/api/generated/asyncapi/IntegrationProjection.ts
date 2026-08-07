import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface IntegrationProjection {
  definitionRef: string;
  definitionVersion: number;
  capabilities: string[];
  credentialBindingIds: string[];
  endpointRef: string;
  ownership: ConfigurationOwnershipProjection;
}
export { IntegrationProjection };