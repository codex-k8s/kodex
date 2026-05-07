package catalog

import "fmt"

var (
	queryManifestSnapshotCreate      = mustLoadQuery("manifest_snapshot__create")
	queryManifestSnapshotGetLatest   = mustLoadQuery("manifest_snapshot__get_latest")
	queryPackageCreate               = mustLoadQuery("package__create")
	queryPackageGetByID              = mustLoadQuery("package__get_by_id")
	queryPackageList                 = mustLoadQuery("package__list")
	queryPackageSourceCreate         = mustLoadQuery("package_source__create")
	queryPackageSourceGetByID        = mustLoadQuery("package_source__get_by_id")
	queryPackageSourceList           = mustLoadQuery("package_source__list")
	queryPackageVersionCreate        = mustLoadQuery("package_version__create")
	queryPackageVersionGetByID       = mustLoadQuery("package_version__get_by_id")
	queryPackageVersionList          = mustLoadQuery("package_version__list")
	queryPricingMetadataCreate       = mustLoadQuery("pricing_metadata__create")
	queryPricingMetadataGetByPackage = mustLoadQuery("pricing_metadata__get_by_package")
	queryPricingMetadataUpdate       = mustLoadQuery("pricing_metadata__update")
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
