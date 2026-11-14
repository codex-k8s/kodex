package query

// ModelDiscoveryFilter carries only informational catalog selectors; every selector and grouping choice participates in the cursor digest and never grants model execution.
// ModelDiscoveryFilter содержит только справочные селекторы каталога; каждый селектор и способ группировки включается в cursor digest и не разрешает запуск модели.
type ModelDiscoveryFilter struct {
	ProviderID, Family, InputModality, OutputModality, Tools, Reasoning, Cache, GroupBy string
	MinimumContextTokens                                                                int64
}
