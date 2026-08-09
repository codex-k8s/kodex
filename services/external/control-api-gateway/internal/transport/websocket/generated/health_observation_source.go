
package generated

type HealthObservationSource uint

const (
  HealthObservationSourceControlPlane HealthObservationSource = iota
  HealthObservationSourceInteractionGateway
  HealthObservationSourceIntegrationGateway
)

// Value returns the value of the enum.
func (op HealthObservationSource) Value() any {
	if op >= HealthObservationSource(len(HealthObservationSourceValues)) {
		return nil
	}
	return HealthObservationSourceValues[op]
}

var HealthObservationSourceValues = []any{"CONTROL_PLANE","INTERACTION_GATEWAY","INTEGRATION_GATEWAY"}
var ValuesToHealthObservationSource = map[any]HealthObservationSource{
  HealthObservationSourceValues[HealthObservationSourceControlPlane]: HealthObservationSourceControlPlane,
  HealthObservationSourceValues[HealthObservationSourceInteractionGateway]: HealthObservationSourceInteractionGateway,
  HealthObservationSourceValues[HealthObservationSourceIntegrationGateway]: HealthObservationSourceIntegrationGateway,
}
