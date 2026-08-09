
package generated

type OwnerGateProjection struct {
  ProcessRunId string
  ResultSha256 string
  ExpiresAt string
  Decision *OwnerGateDecision
  SessionId string
  TurnId string
  Attempt int
  ImmutableInputSha256 string
  DeliveryState *OwnerGateDeliveryState
  Resolvable bool
  DeliveredAt string
  NextAction *OwnerGateNextAction
}