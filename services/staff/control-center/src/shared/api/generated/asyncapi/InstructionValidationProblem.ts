
interface InstructionValidationProblem {
  code: string;
  field: string;
  line: number;
  column: number;
  message: string;
}
export { InstructionValidationProblem };