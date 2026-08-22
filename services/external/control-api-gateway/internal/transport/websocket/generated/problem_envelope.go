package generated

type ProblemEnvelope struct {
	ReservedType string
	Status       int
	Code         *ProblemCode
	Title        string
	Retryable    bool
}
