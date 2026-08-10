import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface PromptProfileProjection {
  revision: number;
  contentSha256: string;
  sourceStatus: string;
  locale: string;
  ownership: ConfigurationOwnershipProjection;
}
export { PromptProfileProjection };