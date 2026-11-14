package entity

// ModelDiscoverySource binds the imported artifact and schema independently; source revision identifies inspected source, not an account observation or a reproduced upstream build.
// ModelDiscoverySource раздельно закрепляет импортированный артефакт и схему; source revision обозначает проверенный исходник, а не наблюдение аккаунта или воспроизведённую сборку upstream.
type ModelDiscoverySource struct {
	Schema                   string `json:"schema"`
	Source                   string `json:"source"`
	License                  string `json:"license"`
	SourceRevision           string `json:"source_revision"`
	UpstreamSchema           string `json:"upstream_schema"`
	SchemaSHA256             string `json:"schema_sha256"`
	UpstreamSHA256           string `json:"upstream_sha256"`
	SnapshotSHA256           string `json:"snapshot_sha256"`
	SourceURL                string `json:"source_url"`
	ModelCount               int    `json:"model_count"`
	OverlaySHA256            string `json:"overlay_sha256"`
	CatalogSHA256            string `json:"catalog_sha256"`
	RetrievedAt              string `json:"retrieved_at"`
	ProjectionSchema         string `json:"projection_schema"`
	ProjectionSchemaSHA256   string `json:"projection_schema_sha256"`
	UpstreamSchemaValidation string `json:"upstream_schema_validation"`
}

// ModelDiscoveryAPI is inert upstream metadata used only to compare an independently configured exact route; SDK names never cause package installation.
// ModelDiscoveryAPI — пассивные метаданные upstream для сравнения отдельно настроенного точного маршрута; имена SDK не приводят к установке пакетов.
type ModelDiscoveryAPI struct {
	Protocol string `json:"protocol"`
	BaseURL  string `json:"base_url"`
	SDK      string `json:"sdk"`
	Path     string `json:"path,omitempty"`
}

// ModelDiscoveryRecord preserves provider/model identity and informational capabilities; the five status dimensions do not form an execution-authorization ladder.
// ModelDiscoveryRecord сохраняет пару provider/model и справочные возможности; пять измерений статуса не образуют последовательность получения права запуска.
type ModelDiscoveryRecord struct {
	ProviderID                               string              `json:"provider_id"`
	ProviderName                             string              `json:"provider_name"`
	ModelID                                  string              `json:"model_id"`
	Name                                     string              `json:"name"`
	Type                                     string              `json:"type"`
	ContextTokens                            int64               `json:"context_tokens"`
	OutputTokens                             int64               `json:"output_tokens"`
	Tools                                    string              `json:"tools"`
	Reasoning                                string              `json:"reasoning"`
	Family                                   string              `json:"family"`
	Cache                                    string              `json:"cache"`
	CacheImplicit                            string              `json:"cache_implicit"`
	CacheExplicit                            string              `json:"cache_explicit"`
	ContextKnown                             bool                `json:"context_known"`
	OutputKnown                              bool                `json:"output_known"`
	ReasoningEfforts                         []string            `json:"reasoning_efforts"`
	InputModalities                          []string            `json:"input_modalities"`
	OutputModalities                         []string            `json:"output_modalities"`
	UpstreamStatus                           string              `json:"upstream_status"`
	APIs                                     []ModelDiscoveryAPI `json:"apis"`
	Known, Adapter, Account, Allowed, Tested string              `json:"-"`
	RouteConfigured                          bool                `json:"-"`
}

// ModelDiscoveryPage returns a revision-bound search page; runtime configuration and credentials remain in their existing authoritative owners.
// ModelDiscoveryPage возвращает страницу поиска, связанную с ревизией; runtime configuration и credentials остаются у прежних авторитетных владельцев.
type ModelDiscoveryPage struct {
	Items         []ModelDiscoveryRecord
	Source        ModelDiscoverySource
	Total         int64
	NextPageToken string
	GroupBy       string
}
