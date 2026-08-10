import {ResourceKind} from './ResourceKind';
import {LifecycleState} from './LifecycleState';
import {ResourceSpecProjection} from './ResourceSpecProjection';
import {ResourceNextAction} from './ResourceNextAction';
interface Resource {
  id: string;
  kind: ResourceKind;
  reservedName: string;
  state: LifecycleState;
  version: number;
  projectionSha256: string;
  projectId?: string;
  parentId?: string;
  spec: ResourceSpecProjection;
  createdAt: string;
  updatedAt: string;
  nextActions: ResourceNextAction[];
}
export { Resource };