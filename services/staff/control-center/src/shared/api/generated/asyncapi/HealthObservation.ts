import {HealthObservationSource} from './HealthObservationSource';
import {HealthObservationStatus} from './HealthObservationStatus';
interface HealthObservation {
  source: HealthObservationSource;
  component: string;
  reservedStatus: HealthObservationStatus;
  value: number;
  version: number;
  digestSha256?: string;
  observedAt: string;
}
export { HealthObservation };