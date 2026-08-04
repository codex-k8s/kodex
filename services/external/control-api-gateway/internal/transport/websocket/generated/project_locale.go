
package generated

type ProjectLocale uint

const (
  ProjectLocaleRu ProjectLocale = iota
  ProjectLocaleEn
)

// Value returns the value of the enum.
func (op ProjectLocale) Value() any {
	if op >= ProjectLocale(len(ProjectLocaleValues)) {
		return nil
	}
	return ProjectLocaleValues[op]
}

var ProjectLocaleValues = []any{"ru","en"}
var ValuesToProjectLocale = map[any]ProjectLocale{
  ProjectLocaleValues[ProjectLocaleRu]: ProjectLocaleRu,
  ProjectLocaleValues[ProjectLocaleEn]: ProjectLocaleEn,
}
