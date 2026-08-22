import {PlatformEventName} from './PlatformEventName';
import {PlatformResourceKind} from './PlatformResourceKind';
interface PlatformInvalidatedEnvelope {
  reservedType: 'PLATFORM_INVALIDATED';
  requestRef: string;
  sequence: number;
  eventName: PlatformEventName;
  kind: PlatformResourceKind;
}
export { PlatformInvalidatedEnvelope };