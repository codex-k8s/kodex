
package generated

type ConfigurationManagedBy uint

const (
  ConfigurationManagedByUi ConfigurationManagedBy = iota
  ConfigurationManagedByGit
)

// Value returns the value of the enum.
func (op ConfigurationManagedBy) Value() any {
	if op >= ConfigurationManagedBy(len(ConfigurationManagedByValues)) {
		return nil
	}
	return ConfigurationManagedByValues[op]
}

var ConfigurationManagedByValues = []any{"ui","git"}
var ValuesToConfigurationManagedBy = map[any]ConfigurationManagedBy{
  ConfigurationManagedByValues[ConfigurationManagedByUi]: ConfigurationManagedByUi,
  ConfigurationManagedByValues[ConfigurationManagedByGit]: ConfigurationManagedByGit,
}
