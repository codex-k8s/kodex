import {UserSummary} from './UserSummary';
import {OwnerGateState} from './OwnerGateState';
import {OwnerGateDecision} from './OwnerGateDecision';
import {NextAction} from './NextAction';
interface OwnerGate {
  ref: string;
  version: number;
  projectRef: string;
  runRef: string;
  nodeRef: string;
  title: string;
  contextSummary: string;
  consequencesSummary: string;
  requestedBy: UserSummary;
  state: OwnerGateState;
  allowedDecisions: OwnerGateDecision[];
  decision?: OwnerGateDecision;
  decisionComment?: string;
  openedAt: string;
  decidedAt?: string;
  artifactRefs: string[];
  nextActions: NextAction[];
}
export { OwnerGate };