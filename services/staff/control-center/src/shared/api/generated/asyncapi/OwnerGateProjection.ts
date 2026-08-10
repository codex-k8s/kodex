import {OwnerGateDecision} from './OwnerGateDecision';
import {OwnerGateDeliveryState} from './OwnerGateDeliveryState';
import {OwnerGateNextAction} from './OwnerGateNextAction';
interface OwnerGateProjection {
  processRunId: string;
  resultSha256: string;
  expiresAt: string;
  decision: OwnerGateDecision;
  sessionId: string;
  turnId: string;
  attempt: number;
  immutableInputSha256: string;
  deliveryState: OwnerGateDeliveryState;
  resolvable: boolean;
  deliveredAt?: string;
  nextAction: OwnerGateNextAction;
}
export { OwnerGateProjection };