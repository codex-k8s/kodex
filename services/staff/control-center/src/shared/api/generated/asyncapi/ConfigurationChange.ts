import {ConfigurationChangeAction} from './ConfigurationChangeAction';
import {ResourceKind} from './ResourceKind';
import {ConfigurationChangeOutcome} from './ConfigurationChangeOutcome';
interface ConfigurationChange {
  id: string;
  action: ConfigurationChangeAction;
  resourceId: string;
  resourceKind: ResourceKind;
  resourceVersion: number;
  outcome: ConfigurationChangeOutcome;
  actorId: string;
  correlationId: string;
  policyRevision: number;
  occurredAt: string;
}
export { ConfigurationChange };