
package generated

type RunEvent struct {
  Ref string
  RunRef string
  Sequence int
  ReservedType *RunEventType
  NodeRef string
  EdgeRef string
  GateRef string
  ArtifactRef string
  Summary string
  Progress string
  RunState *RunState
  NodeState *RunNodeState
  OccurredAt string
  GraphRevision int
  Run *RunDelta
  Node *RunNode
  Edge *RunEdge
  Gate *OwnerGate
  Artifact *Artifact
}