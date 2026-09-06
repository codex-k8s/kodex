// Package query содержит owner-scoped запросы чтения.
package query

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

type Page struct {
	Size  int32
	Token string
}

type Filter struct {
	ProviderUsage                                                                   *entity.ProviderAccountUsageContext `json:",omitempty"`
	VFSKinds                                                                        []string                            `json:",omitempty"`
	TargetType, TargetRef                                                           string                              `json:",omitempty"`
	ResumableSessionsOnly                                                           bool                                `json:",omitempty"`
	ExpectedCatalogRevision, ExpectedCatalogDigest                                  string                              `json:",omitempty"`
	TemplateContext                                                                 *TemplateVariableContext            `json:",omitempty"`
	ProjectRef, ResourceRef, Query, State, Category, DefinitionKey, Action, Outcome string
	ArtifactType, ScanState, SourceKind                                             string
	SourceKinds                                                                     []string `json:",omitempty"`
	States                                                                          []string
	AfterSequence                                                                   int64
	Limit                                                                           int32
	Page                                                                            Page
}

type TemplateVariableContext struct {
	AgentRef, RuntimeRevisionRef                 string
	TargetKind, TargetRef, ExpectedContextDigest string
	Preview                                      PromptPreviewContext
}
