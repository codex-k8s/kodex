
package generated

type ConfigurationChange struct {
  Id string
  Action *ConfigurationChangeAction
  ResourceId string
  ResourceKind *ResourceKind
  ResourceVersion int
  Outcome *ConfigurationChangeOutcome
  ActorId string
  CorrelationId string
  PolicyRevision int
  OccurredAt string
}