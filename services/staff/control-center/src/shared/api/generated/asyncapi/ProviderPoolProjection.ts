import {ProviderPoolPolicy} from './ProviderPoolPolicy';
import {ProviderPoolBindingProjection} from './ProviderPoolBindingProjection';
interface ProviderPoolProjection {
  policy: ProviderPoolPolicy;
  policyRevision: number;
  observationMaxAgeSeconds: number;
  bindings: ProviderPoolBindingProjection[];
}
export { ProviderPoolProjection };