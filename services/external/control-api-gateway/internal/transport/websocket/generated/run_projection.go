
package generated

type RunProjection struct {
  RunRef string
  DisplayName string
  Version int
  State *LifecycleState
  Workspace *OwnerDisplayValue
  Trigger *OwnerDisplayValue
  RuntimeStatus *OwnerDisplayValue
  Attempt int
  StartedAt string
  UpdatedAt string
  DurationSeconds int
  NextActions []RunNextAction
  Initiator *OwnerDisplayValue
  Agent *OwnerDisplayValue
  Role *OwnerDisplayValue
  Model *OwnerDisplayValue
  Provider *OwnerDisplayValue
}