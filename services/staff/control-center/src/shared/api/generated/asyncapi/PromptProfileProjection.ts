import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface PromptProfileProjection {
  revision: number;
  contentSha256: string;
  sourceRef: string;
  locale: string;
  ownership: ConfigurationOwnershipProjection;
}
export { PromptProfileProjection };