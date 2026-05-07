package catalog

import "fmt"

var (
	queryCommandResultCreate         = mustLoadQuery("command_result__create")
	queryCommandResultGet            = mustLoadQuery("command_result__get")
	queryManifestSnapshotCreate      = mustLoadQuery("manifest_snapshot__create")
	queryManifestSnapshotGetLatest   = mustLoadQuery("manifest_snapshot__get_latest")
	queryOutboxEventClaim            = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate           = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed       = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanent    = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished    = mustLoadQuery("outbox_event__mark_published")
	queryPackageCreate               = mustLoadQuery("package__create")
	queryPackageGetByID              = mustLoadQuery("package__get_by_id")
	queryPackageInstallationCreate   = mustLoadQuery("package_installation__create")
	queryPackageInstallationGetByID  = mustLoadQuery("package_installation__get_by_id")
	queryPackageInstallationList     = mustLoadQuery("package_installation__list")
	queryPackageInstallationUpdate   = mustLoadQuery("package_installation__update")
	queryPackageList                 = mustLoadQuery("package__list")
	queryPackageSourceCreate         = mustLoadQuery("package_source__create")
	queryPackageSourceGetByID        = mustLoadQuery("package_source__get_by_id")
	queryPackageSourceList           = mustLoadQuery("package_source__list")
	queryPackageSourceUpdate         = mustLoadQuery("package_source__update")
	queryPackageSecretSchemaCreate   = mustLoadQuery("package_secret_schema__create")
	queryPackageSecretSchemaLatest   = mustLoadQuery("package_secret_schema__get_latest")
	queryPackageVerificationCreate   = mustLoadQuery("package_verification__create")
	queryPackageVerificationList     = mustLoadQuery("package_verification__list")
	queryPackageVersionCreate        = mustLoadQuery("package_version__create")
	queryPackageVersionGetByID       = mustLoadQuery("package_version__get_by_id")
	queryPackageVersionList          = mustLoadQuery("package_version__list")
	queryPackageVersionVerification  = mustLoadQuery("package_version__set_verification")
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
