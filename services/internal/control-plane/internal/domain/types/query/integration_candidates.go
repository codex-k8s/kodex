package query

type IntegrationCandidateContext struct {
	ConnectionRef, ProjectRef, RecipientKind, RecipientRef string
	CapabilityKey, WorkflowRef, StepKey                    string
}

type IntegrationCandidates struct {
	Purpose string
	Context IntegrationCandidateContext
	Filter  Filter
}
