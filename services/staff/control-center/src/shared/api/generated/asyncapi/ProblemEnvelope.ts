import {ProblemMessageType} from './ProblemMessageType';
interface ProblemEnvelope {
  reservedType: ProblemMessageType;
  requestId: string;
  code: string;
  retryable: boolean;
}
export { ProblemEnvelope };