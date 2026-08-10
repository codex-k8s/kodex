import {InstructionVersionState} from './InstructionVersionState';
import {InstructionValidationProblem} from './InstructionValidationProblem';
import {ConfigurationOwnershipProjection} from './ConfigurationOwnershipProjection';
interface InstructionSetProjection {
  stableKey: string;
  locale: string;
  currentVersion: number;
  publishedVersion: number;
  content: string;
  contentSha256: string;
  versionState: InstructionVersionState;
  validationSucceeded: boolean;
  validationProblems: InstructionValidationProblem[];
  ownership: ConfigurationOwnershipProjection;
}
export { InstructionSetProjection };