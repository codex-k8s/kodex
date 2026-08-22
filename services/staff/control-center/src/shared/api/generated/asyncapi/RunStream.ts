import {SnapshotEnvelope} from './SnapshotEnvelope';
import {EventEnvelope} from './EventEnvelope';
import {ReadyEnvelope} from './ReadyEnvelope';
import {ResyncEnvelope} from './ResyncEnvelope';
import {HeartbeatEnvelope} from './HeartbeatEnvelope';
import {ProblemEnvelope} from './ProblemEnvelope';
type RunStream = SnapshotEnvelope | EventEnvelope | ReadyEnvelope | ResyncEnvelope | HeartbeatEnvelope | ProblemEnvelope;
export { RunStream };