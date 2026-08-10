import {RunProjection} from './RunProjection';
import {Resource} from './Resource';
import {IncidentProjection} from './IncidentProjection';
import {ConfigurationChange} from './ConfigurationChange';
import {MattermostTeamProjection} from './MattermostTeamProjection';
import {ProviderConnectionProjection} from './ProviderConnectionProjection';
import {IntegrationConfigurationProjection} from './IntegrationConfigurationProjection';
import {IntegrationApproval} from './IntegrationApproval';
import {HealthObservation} from './HealthObservation';
interface SnapshotItems {
  runs?: RunProjection[];
  resources?: Resource[];
  incidents?: IncidentProjection[];
  configurationChanges?: ConfigurationChange[];
  teams?: MattermostTeamProjection[];
  providerConnections?: ProviderConnectionProjection[];
  integrationConfigurations?: IntegrationConfigurationProjection[];
  approvals?: IntegrationApproval[];
  health?: HealthObservation[];
}
export { SnapshotItems };