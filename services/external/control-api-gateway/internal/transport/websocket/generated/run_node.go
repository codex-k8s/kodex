
package generated

type RunNode struct {
  Ref string
  RunRef string
  ParentNodeRef string
  ReservedType *RunNodeType
  State *RunNodeState
  DisplayName string
  Role string
  AgentRef string
  TurnRef string
  Attempt int
  InputSummary string
  ProgressSummary string
  IntegrationNames []string
  ArtifactRefs []string
  ChildRunRefs []string
  CallbackSummary string
  SafeErrorCode string
  SafeErrorMessage string
  CreatedAt string
  StartedAt string
  FinishedAt string
  NextActions []NextAction
}