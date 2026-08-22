import {RunEvent} from './RunEvent';
interface EventEnvelope {
  reservedType: 'RUN_EVENT';
  requestRef: string;
  runRef: string;
  sequence: number;
  reservedEvent: RunEvent;
}
export { EventEnvelope };