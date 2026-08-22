import {RunEventType} from './RunEventType';
import {RunState} from './RunState';
import {RunNodeState} from './RunNodeState';
interface RunEvent {
  ref: string;
  runRef: string;
  sequence: number;
  reservedType: RunEventType;
  nodeRef?: string;
  edgeRef?: string;
  gateRef?: string;
  artifactRef?: string;
  summary: string;
  progress?: string;
  runState?: RunState;
  nodeState?: RunNodeState;
  occurredAt: string;
}
export { RunEvent };