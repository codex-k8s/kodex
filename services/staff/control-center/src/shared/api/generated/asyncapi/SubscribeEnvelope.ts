import {SubscribeMessageType} from './SubscribeMessageType';
import {ProjectionChannel} from './ProjectionChannel';
import {ResourceKind} from './ResourceKind';
interface SubscribeEnvelope {
  reservedType: SubscribeMessageType;
  requestId: string;
  channels: ProjectionChannel[];
  resourceKinds?: ResourceKind[];
}
export { SubscribeEnvelope };