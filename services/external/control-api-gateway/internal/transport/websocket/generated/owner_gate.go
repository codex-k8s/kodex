
package generated

type OwnerGate struct {
  Ref string
  Version int
  ProjectRef string
  RunRef string
  NodeRef string
  Title string
  ContextSummary string
  ConsequencesSummary string
  RequestedBy *UserSummary
  State *OwnerGateState
  AllowedDecisions []OwnerGateDecision
  Decision *OwnerGateDecision
  DecisionComment string
  OpenedAt string
  DecidedAt string
  ArtifactRefs []string
  NextActions []NextAction
}