package generated

type ProblemCode uint

const (
	ProblemCodeUnauthorized ProblemCode = iota
	ProblemCodeForbidden
	ProblemCodeRunNotFound
	ProblemCodeRateLimited
	ProblemCodeGapUnrecoverable
	ProblemCodeBackpressure
	ProblemCodeInternal
)

// Value returns the value of the enum.
func (op ProblemCode) Value() any {
	if op >= ProblemCode(len(ProblemCodeValues)) {
		return nil
	}
	return ProblemCodeValues[op]
}

var ProblemCodeValues = []any{"UNAUTHORIZED", "FORBIDDEN", "RUN_NOT_FOUND", "RATE_LIMITED", "GAP_UNRECOVERABLE", "BACKPRESSURE", "INTERNAL"}
var ValuesToProblemCode = map[any]ProblemCode{
	ProblemCodeValues[ProblemCodeUnauthorized]:     ProblemCodeUnauthorized,
	ProblemCodeValues[ProblemCodeForbidden]:        ProblemCodeForbidden,
	ProblemCodeValues[ProblemCodeRunNotFound]:      ProblemCodeRunNotFound,
	ProblemCodeValues[ProblemCodeRateLimited]:      ProblemCodeRateLimited,
	ProblemCodeValues[ProblemCodeGapUnrecoverable]: ProblemCodeGapUnrecoverable,
	ProblemCodeValues[ProblemCodeBackpressure]:     ProblemCodeBackpressure,
	ProblemCodeValues[ProblemCodeInternal]:         ProblemCodeInternal,
}
