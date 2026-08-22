import {RunNodeType} from './RunNodeType';
import {RunNodeState} from './RunNodeState';
import {NextAction} from './NextAction';
interface RunNode {
  ref: string;
  runRef: string;
  parentNodeRef?: string;
  reservedType: RunNodeType;
  state: RunNodeState;
  displayName: string;
  role?: string;
  agentRef?: string;
  turnRef?: string;
  attempt: number;
  inputSummary?: string;
  progressSummary?: string;
  integrationNames?: string[];
  artifactRefs: string[];
  childRunRefs: string[];
  callbackSummary?: string;
  safeErrorCode?: string;
  safeErrorMessage?: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  nextActions: NextAction[];
}
export { RunNode };