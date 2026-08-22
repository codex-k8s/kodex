import {ResyncReason} from './ResyncReason';
interface ResyncEnvelope {
  reservedType: 'RESYNC_REQUIRED';
  requestRef: string;
  runRef: string;
  expectedAfterSequence: number;
  reason: ResyncReason;
}
export { ResyncEnvelope };