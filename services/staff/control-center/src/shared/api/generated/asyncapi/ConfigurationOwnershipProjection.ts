import {ConfigurationManagedBy} from './ConfigurationManagedBy';
import {ConfigurationDrift} from './ConfigurationDrift';
interface ConfigurationOwnershipProjection {
  managedBy: ConfigurationManagedBy;
  source: string;
  revision: number;
  sourceSha256?: string;
  drift: ConfigurationDrift;
}
export { ConfigurationOwnershipProjection };