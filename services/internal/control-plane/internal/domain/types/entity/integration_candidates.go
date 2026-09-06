package entity

type IntegrationCandidateContext struct {
	ConnectionRef, ProjectRef, RecipientKind, RecipientRef string
	CapabilityKey, WorkflowRef, StepKey                    string
}

type IntegrationCandidatePins struct {
	ContextDigest, DefinitionVersion, DefinitionDigest  string
	ConnectionVersion, ProjectVersion, RecipientVersion int64
	WorkflowRevisionRef                                 string
}

type IntegrationConnectionCandidate struct {
	ConnectionRef, Name, DefinitionKey, ProviderName, CredentialKind string
	ProjectRef, Reason                                               string
	ResourceScope                                                    map[string]string
	Grantable, Usable                                                bool
	Pins                                                             IntegrationCandidatePins
}

type IntegrationProjectCandidate struct {
	ProjectRef, Name, Reason string
	Grantable                bool
	Pins                     IntegrationCandidatePins
}

type IntegrationRecipientCandidate struct {
	RecipientRef, Name, RecipientKind, ProjectRef, Reason string
	Grantable                                             bool
	Pins                                                  IntegrationCandidatePins
}

type IntegrationCapabilityCandidate struct {
	Capability          IntegrationCapability
	Grantable           bool
	Reason              string
	CurrentGrantRef     string
	CurrentGrantVersion int64
	Pins                IntegrationCandidatePins
}

type IntegrationCandidatePage struct {
	NextPageToken, ContextDigest string
	Total                        int64
	Context                      IntegrationCandidateContext
	Pins                         IntegrationCandidatePins
}

type IntegrationConnectionCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationConnectionCandidate
}

type IntegrationProjectCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationProjectCandidate
}

type IntegrationRecipientCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationRecipientCandidate
}

type IntegrationCapabilityCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationCapabilityCandidate
}
