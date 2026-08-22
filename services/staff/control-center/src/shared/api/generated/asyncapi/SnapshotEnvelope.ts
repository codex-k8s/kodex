import {RunGraph} from './RunGraph';
interface SnapshotEnvelope {
  reservedType: 'GRAPH_SNAPSHOT';
  requestRef: string;
  runRef: string;
  sequence: number;
  snapshot: RunGraph;
}
export { SnapshotEnvelope };