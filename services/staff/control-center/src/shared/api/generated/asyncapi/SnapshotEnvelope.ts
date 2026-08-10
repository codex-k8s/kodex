import {SnapshotMessageType} from './SnapshotMessageType';
import {ProjectionChannel} from './ProjectionChannel';
import {SnapshotItems} from './SnapshotItems';
interface SnapshotEnvelope {
  reservedType: SnapshotMessageType;
  requestId: string;
  channel: ProjectionChannel;
  sequence: number;
  snapshotId: string;
  complete: boolean;
  serverTime: string;
  items: SnapshotItems;
}
export { SnapshotEnvelope };