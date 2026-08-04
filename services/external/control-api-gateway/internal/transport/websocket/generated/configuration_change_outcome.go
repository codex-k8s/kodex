
package generated

type ConfigurationChangeOutcome uint

const (
  ConfigurationChangeOutcomeSucceeded ConfigurationChangeOutcome = iota
)

// Value returns the value of the enum.
func (op ConfigurationChangeOutcome) Value() any {
	if op >= ConfigurationChangeOutcome(len(ConfigurationChangeOutcomeValues)) {
		return nil
	}
	return ConfigurationChangeOutcomeValues[op]
}

var ConfigurationChangeOutcomeValues = []any{"succeeded"}
var ValuesToConfigurationChangeOutcome = map[any]ConfigurationChangeOutcome{
  ConfigurationChangeOutcomeValues[ConfigurationChangeOutcomeSucceeded]: ConfigurationChangeOutcomeSucceeded,
}
