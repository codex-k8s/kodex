package provider

import "fmt"

var (
	queryAccountRuntimeStateGet    = mustLoadQuery("account_runtime_state__get")
	queryAccountRuntimeStateList   = mustLoadQuery("account_runtime_state__list")
	queryAccountRuntimeStateUpsert = mustLoadQuery("account_runtime_state__upsert")
	queryLimitSnapshotList         = mustLoadQuery("limit_snapshot__list")
	queryLimitSnapshotUpsert       = mustLoadQuery("limit_snapshot__upsert")
	queryProviderOperationInsert   = mustLoadQuery("provider_operation__insert")
	queryProviderOperationList     = mustLoadQuery("provider_operation__list")
)

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err != nil {
		panic(err)
	}
	return query
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load sql query %s: %w", name, err)
	}
	return string(data), nil
}
