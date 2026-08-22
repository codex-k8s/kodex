import {ProblemCode} from './ProblemCode';
interface ProblemEnvelope {
  reservedType: 'PROBLEM';
  reservedStatus: number;
  code: ProblemCode;
  title: string;
  retryable: boolean;
}
export { ProblemEnvelope };