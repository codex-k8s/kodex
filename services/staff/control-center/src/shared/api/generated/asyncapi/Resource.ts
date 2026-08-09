import {ResourceKind} from './ResourceKind';
import {LifecycleState} from './LifecycleState';
import {ResourceSpecProjection} from './ResourceSpecProjection';
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
}
export { Resource };