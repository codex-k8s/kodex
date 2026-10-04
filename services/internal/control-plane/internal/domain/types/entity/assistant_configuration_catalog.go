package entity

type AssistantRuntimeProfilePin struct {
	Ref             string `json:"ref"`
	Version         int64  `json:"version"`
	RuntimeRevision string `json:"runtimeRevision"`
}

type AssistantConfigurationCatalogRequest struct {
	Kind, AssistantRef, Query, AccountRef, RuntimeProfileRef string
	Offset                                                   int32
}

type AssistantConfigurationCatalogEntry struct {
	Ref, Name, Provider, Model, Reference, ManifestDigest       string
	ScopeKind, OrganizationRef, ProjectRef, AssistantProfileRef string
	CatalogRevision, CatalogDigest, DefaultReasoningEffort      string
	RuntimeEnvironmentRef                                       string
	ReasoningEfforts                                            []string
	Version, RecipeGeneration                                   int64
}

type AssistantConfigurationCatalogResponse struct {
	Kind, AssistantRef, ScopeKind, OrganizationRef, ProjectRef, AssistantProfileRef string
	Entries                                                                         []AssistantConfigurationCatalogEntry
	NextOffset                                                                      int32
}
