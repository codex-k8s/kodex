
package generated

type ConfigurationDrift uint

const (
  ConfigurationDriftNotApplicable ConfigurationDrift = iota
  ConfigurationDriftInSync
  ConfigurationDriftDrifted
  ConfigurationDriftUnknown
)

// Value returns the value of the enum.
func (op ConfigurationDrift) Value() any {
	if op >= ConfigurationDrift(len(ConfigurationDriftValues)) {
		return nil
	}
	return ConfigurationDriftValues[op]
}

var ConfigurationDriftValues = []any{"NOT_APPLICABLE","IN_SYNC","DRIFTED","UNKNOWN"}
var ValuesToConfigurationDrift = map[any]ConfigurationDrift{
  ConfigurationDriftValues[ConfigurationDriftNotApplicable]: ConfigurationDriftNotApplicable,
  ConfigurationDriftValues[ConfigurationDriftInSync]: ConfigurationDriftInSync,
  ConfigurationDriftValues[ConfigurationDriftDrifted]: ConfigurationDriftDrifted,
  ConfigurationDriftValues[ConfigurationDriftUnknown]: ConfigurationDriftUnknown,
}
