import {PlatformInvalidatedEnvelope} from './PlatformInvalidatedEnvelope';
import {PlatformReadyEnvelope} from './PlatformReadyEnvelope';
import {PlatformResyncEnvelope} from './PlatformResyncEnvelope';
import {PlatformHeartbeatEnvelope} from './PlatformHeartbeatEnvelope';
import {ProblemEnvelope} from './ProblemEnvelope';
type PlatformStream = PlatformInvalidatedEnvelope | PlatformReadyEnvelope | PlatformResyncEnvelope | PlatformHeartbeatEnvelope | ProblemEnvelope;
export { PlatformStream };