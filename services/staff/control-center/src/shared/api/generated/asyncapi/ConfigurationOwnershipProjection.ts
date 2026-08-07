import {ConfigurationManagedBy} from './ConfigurationManagedBy';
interface ConfigurationOwnershipProjection {
  managedBy: ConfigurationManagedBy;
  source: string;
  revision: number;
}
export { ConfigurationOwnershipProjection };