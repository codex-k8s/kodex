import {IntegrationEffectKind} from './IntegrationEffectKind';
import {IntegrationConfigurationState} from './IntegrationConfigurationState';
interface IntegrationConfigurationProjection {
  configurationRef: string;
  stableKey: string;
  version: number;
  digestSha256: string;
  definitionRef: string;
  definitionVersion: number;
  definitionDigestSha256: string;
  connectionRef: string;
  connectionVersion: number;
  connectionGeneration: number;
  capabilities: string[];
  capabilityDigestSha256: string;
  effectKind: IntegrationEffectKind;
  state: IntegrationConfigurationState;
  updatedAt: string;
}
export { IntegrationConfigurationProjection };