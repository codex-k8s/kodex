import {ProviderPoolPolicy} from './ProviderPoolPolicy';
interface ProviderPoolProjection {
  policy: ProviderPoolPolicy;
  policyRevision: number;
  observationMaxAgeSeconds: number;
  bindingCount: number;
}
export { ProviderPoolProjection };