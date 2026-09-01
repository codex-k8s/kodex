// Package query содержит owner-scoped запросы чтения.
package query

type Page struct {
	Size  int32
	Token string
}

type Filter struct {
	ProjectRef, ResourceRef, Query, State, Category, DefinitionKey, Action, Outcome string
	ArtifactType, ScanState, SourceKind                                             string
	States                                                                          []string
	AfterSequence                                                                   int64
	Limit                                                                           int32
	Page                                                                            Page
}
