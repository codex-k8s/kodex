import {RunState} from './RunState';
import {NextAction} from './NextAction';
interface RunDelta {
  ref: string;
  version: number;
  state: RunState;
  graphRevision: number;
  lastEventSequence: number;
  resultSummary?: string;
  safeErrorCode?: string;
  safeErrorMessage?: string;
  artifactRefs: string[];
  gateRefs: string[];
  startedAt?: string;
  finishedAt?: string;
  nextActions: NextAction[];
}
export { RunDelta };