
package generated

type ProblemMessageType uint

const (
  ProblemMessageTypeProblem ProblemMessageType = iota
)

// Value returns the value of the enum.
func (op ProblemMessageType) Value() any {
	if op >= ProblemMessageType(len(ProblemMessageTypeValues)) {
		return nil
	}
	return ProblemMessageTypeValues[op]
}

var ProblemMessageTypeValues = []any{"PROBLEM"}
var ValuesToProblemMessageType = map[any]ProblemMessageType{
  ProblemMessageTypeValues[ProblemMessageTypeProblem]: ProblemMessageTypeProblem,
}
