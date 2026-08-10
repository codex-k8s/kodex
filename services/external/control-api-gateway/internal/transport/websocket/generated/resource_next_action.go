
package generated

type ResourceNextAction uint

const (
  ResourceNextActionUpdate ResourceNextAction = iota
  ResourceNextActionActivate
  ResourceNextActionPause
  ResourceNextActionArchive
  ResourceNextActionDelete
  ResourceNextActionDetach
  ResourceNextActionCopy
)

// Value returns the value of the enum.
func (op ResourceNextAction) Value() any {
	if op >= ResourceNextAction(len(ResourceNextActionValues)) {
		return nil
	}
	return ResourceNextActionValues[op]
}

var ResourceNextActionValues = []any{"UPDATE","ACTIVATE","PAUSE","ARCHIVE","DELETE","DETACH","COPY"}
var ValuesToResourceNextAction = map[any]ResourceNextAction{
  ResourceNextActionValues[ResourceNextActionUpdate]: ResourceNextActionUpdate,
  ResourceNextActionValues[ResourceNextActionActivate]: ResourceNextActionActivate,
  ResourceNextActionValues[ResourceNextActionPause]: ResourceNextActionPause,
  ResourceNextActionValues[ResourceNextActionArchive]: ResourceNextActionArchive,
  ResourceNextActionValues[ResourceNextActionDelete]: ResourceNextActionDelete,
  ResourceNextActionValues[ResourceNextActionDetach]: ResourceNextActionDetach,
  ResourceNextActionValues[ResourceNextActionCopy]: ResourceNextActionCopy,
}
