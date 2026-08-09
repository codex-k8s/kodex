import {LifecycleState} from './LifecycleState';
import {OwnerDisplayValue} from './OwnerDisplayValue';
import {RunNextAction} from './RunNextAction';
interface RunProjection {
  runRef: string;
  displayName: string;
  version: number;
  state: LifecycleState;
  workspace: OwnerDisplayValue;
  trigger: OwnerDisplayValue;
  runtimeStatus: OwnerDisplayValue;
  attempt: number;
  startedAt?: string;
  updatedAt: string;
  durationSeconds: number;
  nextActions: RunNextAction[];
  initiator: OwnerDisplayValue;
  agent: OwnerDisplayValue;
  role: OwnerDisplayValue;
  model: OwnerDisplayValue;
  provider: OwnerDisplayValue;
}
export { RunProjection };