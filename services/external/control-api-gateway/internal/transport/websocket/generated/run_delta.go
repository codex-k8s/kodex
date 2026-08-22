
package generated

type RunDelta struct {
  Ref string
  Version int
  State *RunState
  GraphRevision int
  LastEventSequence int
  ResultSummary string
  SafeErrorCode string
  SafeErrorMessage string
  ArtifactRefs []string
  GateRefs []string
  StartedAt string
  FinishedAt string
  NextActions []NextAction
}