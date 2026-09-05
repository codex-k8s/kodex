// Package query содержит owner-scoped запросы чтения.
package query

type Page struct {
	Size  int32
	Token string
}

type Filter struct {
	TargetType, TargetRef                                                           string                   `json:",omitempty"`
	ResumableSessionsOnly                                                           bool                     `json:",omitempty"`
	ExpectedCatalogRevision, ExpectedCatalogDigest                                  string                   `json:",omitempty"`
	TemplateContext                                                                 *TemplateVariableContext `json:",omitempty"`
	ProjectRef, ResourceRef, Query, State, Category, DefinitionKey, Action, Outcome string
	ArtifactType, ScanState, SourceKind                                             string
	States                                                                          []string
	AfterSequence                                                                   int64
	Limit                                                                           int32
	Page                                                                            Page
}

type TemplateVariableContext struct {
	AgentRef, RuntimeRevisionRef string
}
