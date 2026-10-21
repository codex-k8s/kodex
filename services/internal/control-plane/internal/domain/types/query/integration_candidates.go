package query

// IntegrationCandidateContext narrows connection candidates to a recipient and
// optional workflow step without granting that recipient a capability.
// IntegrationCandidateContext ограничивает подключения кандидатов получателем
// и необязательным шагом процесса, не выдавая получателю capability.
type IntegrationCandidateContext struct {
	ConnectionRef, ProjectRef, RecipientKind, RecipientRef string
	CapabilityKey, WorkflowRef, StepKey                    string
}

// IntegrationCandidates combines the intended purpose, recipient context and
// general filter for a scoped integration-candidate lookup.
// IntegrationCandidates объединяет назначение, контекст получателя и общий
// фильтр для поиска подходящих интеграций в заданной области.
type IntegrationCandidates struct {
	Purpose string
	Context IntegrationCandidateContext
	Filter  Filter
}
