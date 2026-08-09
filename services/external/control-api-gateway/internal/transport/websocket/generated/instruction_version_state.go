
package generated

type InstructionVersionState uint

const (
  InstructionVersionStateDraft InstructionVersionState = iota
  InstructionVersionStateValidated
  InstructionVersionStatePublished
  InstructionVersionStateRejected
  InstructionVersionStateArchived
)

// Value returns the value of the enum.
func (op InstructionVersionState) Value() any {
	if op >= InstructionVersionState(len(InstructionVersionStateValues)) {
		return nil
	}
	return InstructionVersionStateValues[op]
}

var InstructionVersionStateValues = []any{"DRAFT","VALIDATED","PUBLISHED","REJECTED","ARCHIVED"}
var ValuesToInstructionVersionState = map[any]InstructionVersionState{
  InstructionVersionStateValues[InstructionVersionStateDraft]: InstructionVersionStateDraft,
  InstructionVersionStateValues[InstructionVersionStateValidated]: InstructionVersionStateValidated,
  InstructionVersionStateValues[InstructionVersionStatePublished]: InstructionVersionStatePublished,
  InstructionVersionStateValues[InstructionVersionStateRejected]: InstructionVersionStateRejected,
  InstructionVersionStateValues[InstructionVersionStateArchived]: InstructionVersionStateArchived,
}
