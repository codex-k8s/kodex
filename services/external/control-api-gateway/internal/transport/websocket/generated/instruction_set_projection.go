
package generated

type InstructionSetProjection struct {
  StableKey string
  Locale string
  CurrentVersion int
  PublishedVersion int
  Content string
  ContentSha256 string
  VersionState *InstructionVersionState
  ValidationSucceeded bool
  ValidationProblems []InstructionValidationProblem
  Ownership *ConfigurationOwnershipProjection
}