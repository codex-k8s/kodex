// Package query содержит owner-scoped запросы чтения.
package query

import "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"

// Page carries a bounded read size and opaque continuation token; neither
// field replaces owner-side eligibility checks for returned resources.
// Page содержит размер чтения и непрозрачный токен продолжения; ни одно поле
// не заменяет проверку доступности возвращаемых ресурсов владельцем.
type Page struct {
	Size  int32
	Token string
}

// AssistantConversationFilter extends the common resource filters with an
// assistant scope and reference for listing eligible conversations.
// AssistantConversationFilter дополняет общие условия областью и ссылкой
// помощника для получения списка доступных диалогов.
type AssistantConversationFilter struct {
	Filter
	AssistantScope, AssistantRef string
}

// Filter groups domain-level selection, paging and version pins shared by
// read ports; caller-provided fields are not an authorization context.
// Filter объединяет доменные условия отбора, пагинацию и привязки версий для
// портов чтения; поля вызывающего не являются контекстом полномочий.
type Filter struct {
	RuntimeResourceScopeKind                                                        string                              `json:",omitempty"`
	ProviderUsage                                                                   *entity.ProviderAccountUsageContext `json:",omitempty"`
	VFSKinds                                                                        []string                            `json:",omitempty"`
	TargetType, TargetRef                                                           string                              `json:",omitempty"`
	ResumableSessionsOnly                                                           bool                                `json:",omitempty"`
	MatchAssistantLocalizedDefaultTitle                                             bool                                `json:",omitempty"`
	ExcludeTechnical                                                                bool                                `json:",omitempty"`
	ExpectedCatalogRevision, ExpectedCatalogDigest                                  string                              `json:",omitempty"`
	TemplateContext                                                                 *TemplateVariableContext            `json:",omitempty"`
	ProjectRef, ResourceRef, Query, State, Category, DefinitionKey, Action, Outcome string
	ArtifactType, ScanState, SourceKind                                             string
	SourceKinds                                                                     []string `json:",omitempty"`
	Aliases                                                                         []string `json:",omitempty"`
	States                                                                          []string
	AfterSequence                                                                   int64
	Limit                                                                           int32
	Page                                                                            Page
}

// TemplateVariableContext binds a variable lookup to agent, runtime and target
// hints while the owning service validates the referenced context and digest.
// TemplateVariableContext связывает поиск переменных с подсказками сотрудника,
// runtime и цели; владеющий сервис проверяет контекст и дайджест.
type TemplateVariableContext struct {
	AgentRef, RuntimeRevisionRef                 string
	TargetKind, TargetRef, ExpectedContextDigest string
	Preview                                      PromptPreviewContext
}
