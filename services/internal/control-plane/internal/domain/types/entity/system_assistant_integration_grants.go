package entity

// SystemAssistantIntegrationGrantCandidate describes one capability's grantability and current
// approval policy, scope paths, enabled state, and resource-scope digest for the system assistant. It
// is a candidate projection, not a grant or approval decision.
// SystemAssistantIntegrationGrantCandidate описывает допустимость возможности и текущие политику
// подтверждения, пути области, активность и дайджест ресурсной области системного помощника. Это
// проекция кандидата, а не разрешение или решение о подтверждении.
type SystemAssistantIntegrationGrantCandidate struct {
	Capability                                     IntegrationCapability
	Grantable                                      bool
	Reason, CurrentGrantRef, CurrentApprovalPolicy string
	CurrentGrantVersion                            int64
	CurrentApprovalScopePaths                      []string
	CurrentGrantEnabled                            bool
	ResourceScope                                  map[string]string
	ResourceScopeDigest                            string
}

// SystemAssistantIntegrationGrantCandidates returns a candidate page pinned to the resolved
// organization, assistant and connection versions, and definition evidence. The owner retains scope
// when paging or applying a later grant change.
// SystemAssistantIntegrationGrantCandidates возвращает страницу кандидатов с привязками разрешённых
// организации, версий помощника и подключения и свидетельств определения. Владелец сохраняет область
// при пагинации или последующем изменении разрешения.
type SystemAssistantIntegrationGrantCandidates struct {
	ScopeKind, OrganizationRef, AssistantRef, ConnectionRef string
	AssistantVersion, ConnectionVersion                     int64
	DefinitionVersion, DefinitionDigest                     string
	Items                                                   []SystemAssistantIntegrationGrantCandidate
	Total                                                   int64
	NextPageToken                                           string
}

// ProjectAssistantIntegrationGrantCandidates adds exact project-profile identity and version to the
// common assistant candidate page. The project profile selects the recipient through stored owner
// state, not through an arbitrary caller-supplied agent.
// ProjectAssistantIntegrationGrantCandidates добавляет точные личность и версию проектного профиля к
// общей странице кандидатов помощника. Проектный профиль определяет получателя через сохранённое
// состояние владельца, а не произвольного сотрудника от вызывающего кода.
type ProjectAssistantIntegrationGrantCandidates struct {
	SystemAssistantIntegrationGrantCandidates
	ProjectRef, ProfileRef string
	ProfileVersion         int64
}

// ProjectAssistantIntegrationGrantCatalogEntry pairs a named connection and its definition and
// aggregate-version pins with one assistant capability candidate. The entry supplies selection
// evidence and does not itself change the current grant.
// ProjectAssistantIntegrationGrantCatalogEntry связывает именованное подключение и привязки
// определения и версии агрегата с одним кандидатом возможности помощника. Элемент предоставляет
// свидетельства выбора, но сам не меняет текущее разрешение.
type ProjectAssistantIntegrationGrantCatalogEntry struct {
	ConnectionRef, ConnectionName, DefinitionVersion, DefinitionDigest string
	ConnectionVersion                                                  int64
	Candidate                                                          SystemAssistantIntegrationGrantCandidate
}
