import {SnapshotEnvelope} from './SnapshotEnvelope';
import {EventEnvelope} from './EventEnvelope';
import {ResyncEnvelope} from './ResyncEnvelope';
import {HeartbeatEnvelope} from './HeartbeatEnvelope';
import {ProblemEnvelope} from './ProblemEnvelope';
type RunStream = SnapshotEnvelope | EventEnvelope | ResyncEnvelope | HeartbeatEnvelope | ProblemEnvelope;
export { RunStream };