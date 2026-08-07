import {Resource} from './Resource';
import {RuntimeIncident} from './RuntimeIncident';
import {ConfigurationChange} from './ConfigurationChange';
interface SnapshotItems {
  resources?: Resource[];
  incidents?: RuntimeIncident[];
  configurationChanges?: ConfigurationChange[];
}
export { SnapshotItems };