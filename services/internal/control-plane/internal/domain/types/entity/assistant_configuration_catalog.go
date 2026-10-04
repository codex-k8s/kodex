package entity

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

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
	CurrentConfiguration                                                            *AssistantCurrentConfiguration
}

// Свежая read-модель не содержит materialized secret values или transport metadata.
type AssistantCurrentConfiguration struct {
	AgentVersion                                                             int64
	Configuration                                                            AgentRuntimeConfiguration
	PublishedOverlay                                                         ConfigOverlayVersion
	EnvironmentBinding                                                       AgentRuntimeEnvironmentBinding
	EnvironmentRef                                                           string
	EnvironmentVersion                                                       int64
	Environment                                                              RuntimeEnvironmentVersion
	SecretBindings                                                           []RuntimeSecretBinding
	InstructionTemplateRef, InstructionTemplateDigest, PublishedInstructions string
	SystemCoreRevision, SystemCoreInstructions, OwnerInstructions            string
	OwnerInstructionsRevision                                                int64
	TemplateVariables                                                        []TemplateVariable
	ImageToolInventorySHA256                                                 string
	ImageToolInventory                                                       *runtimecontract.ImageToolInventory
}
