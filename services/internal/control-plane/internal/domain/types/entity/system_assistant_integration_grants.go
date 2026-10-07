package entity

type SystemAssistantIntegrationGrantCandidate struct {
	Capability                                     IntegrationCapability
	Grantable                                      bool
	Reason, CurrentGrantRef, CurrentApprovalPolicy string
	CurrentGrantVersion                            int64
	CurrentApprovalScopePaths                      []string
	CurrentGrantEnabled                            bool
}

type SystemAssistantIntegrationGrantCandidates struct {
	ScopeKind, OrganizationRef, AssistantRef, ConnectionRef string
	AssistantVersion, ConnectionVersion                     int64
	DefinitionVersion, DefinitionDigest                     string
	Items                                                   []SystemAssistantIntegrationGrantCandidate
	Total                                                   int64
	NextPageToken                                           string
}

type ProjectAssistantIntegrationGrantCandidates struct {
	SystemAssistantIntegrationGrantCandidates
	ProjectRef, ProfileRef string
	ProfileVersion         int64
}

type ProjectAssistantIntegrationGrantCatalogEntry struct {
	ConnectionRef, ConnectionName, DefinitionVersion, DefinitionDigest string
	ConnectionVersion                                                  int64
	Candidate                                                          SystemAssistantIntegrationGrantCandidate
}
