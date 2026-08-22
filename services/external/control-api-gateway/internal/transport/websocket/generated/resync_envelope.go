package generated

type ResyncEnvelope struct {
	ReservedType          string
	RequestRef            string
	RunRef                string
	ExpectedAfterSequence int
	Reason                *ResyncReason
}
