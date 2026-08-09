
package generated

type IntegrationConfigurationState uint

const (
  IntegrationConfigurationStateActive IntegrationConfigurationState = iota
  IntegrationConfigurationStateArchived
)

// Value returns the value of the enum.
func (op IntegrationConfigurationState) Value() any {
	if op >= IntegrationConfigurationState(len(IntegrationConfigurationStateValues)) {
		return nil
	}
	return IntegrationConfigurationStateValues[op]
}

var IntegrationConfigurationStateValues = []any{"ACTIVE","ARCHIVED"}
var ValuesToIntegrationConfigurationState = map[any]IntegrationConfigurationState{
  IntegrationConfigurationStateValues[IntegrationConfigurationStateActive]: IntegrationConfigurationStateActive,
  IntegrationConfigurationStateValues[IntegrationConfigurationStateArchived]: IntegrationConfigurationStateArchived,
}
