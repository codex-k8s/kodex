import {ProjectLocale} from './ProjectLocale';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface ProjectProjection {
  slug: string;
  description: string;
  locale: ProjectLocale;
  ownership: ConfigurationOwnershipProjection;
}
export { ProjectProjection };