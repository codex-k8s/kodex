package catalog

import "fmt"

var (
	queryCommandResultCreate         string
	queryCommandResultGet            string
	queryManifestSnapshotCreate      string
	queryManifestSnapshotGetLatest   string
	queryOutboxEventClaim            string
	queryOutboxEventCreate           string
	queryOutboxEventMarkFailed       string
	queryOutboxEventMarkPermanent    string
	queryOutboxEventMarkPublished    string
	queryPackageCreate               string
	queryPackageGetByID              string
	queryPackageInsertIgnore         string
	queryPackageInstallationCreate   string
	queryPackageInstallationGetByID  string
	queryPackageInstallationList     string
	queryPackageInstallationUpdate   string
	queryPackageList                 string
	queryPackageUpdateBySourceSlug   string
	queryPackageSourceCreate         string
	queryPackageSourceGetByID        string
	queryPackageSourceList           string
	queryPackageSourceUpdate         string
	queryPackageSecretSchemaCreate   string
	queryPackageSecretSchemaIgnore   string
	queryPackageSecretSchemaLatest   string
	queryPackageVerificationCreate   string
	queryPackageVerificationList     string
	queryPackageVersionCreate        string
	queryPackageVersionGetByID       string
	queryPackageVersionInsertIgnore  string
	queryPackageVersionList          string
	queryPackageVersionVerification  string
	queryPackageVersionUpdateByLabel string
	queryPricingMetadataCreate       string
	queryPricingMetadataGetByPackage string
	queryPricingMetadataUpdate       string
)

func init() {
	for target, name := range map[*string]string{
		&queryCommandResultCreate:         "command_result__create",
		&queryCommandResultGet:            "command_result__get",
		&queryManifestSnapshotCreate:      "manifest_snapshot__create",
		&queryManifestSnapshotGetLatest:   "manifest_snapshot__get_latest",
		&queryOutboxEventClaim:            "outbox_event__claim",
		&queryOutboxEventCreate:           "outbox_event__create",
		&queryOutboxEventMarkFailed:       "outbox_event__mark_failed",
		&queryOutboxEventMarkPermanent:    "outbox_event__mark_permanently_failed",
		&queryOutboxEventMarkPublished:    "outbox_event__mark_published",
		&queryPackageCreate:               "package__create",
		&queryPackageGetByID:              "package__get_by_id",
		&queryPackageInsertIgnore:         "package__insert_ignore",
		&queryPackageInstallationCreate:   "package_installation__create",
		&queryPackageInstallationGetByID:  "package_installation__get_by_id",
		&queryPackageInstallationList:     "package_installation__list",
		&queryPackageInstallationUpdate:   "package_installation__update",
		&queryPackageList:                 "package__list",
		&queryPackageUpdateBySourceSlug:   "package__update_by_source_slug",
		&queryPackageSourceCreate:         "package_source__create",
		&queryPackageSourceGetByID:        "package_source__get_by_id",
		&queryPackageSourceList:           "package_source__list",
		&queryPackageSourceUpdate:         "package_source__update",
		&queryPackageSecretSchemaCreate:   "package_secret_schema__create",
		&queryPackageSecretSchemaIgnore:   "package_secret_schema__insert_ignore",
		&queryPackageSecretSchemaLatest:   "package_secret_schema__get_latest",
		&queryPackageVerificationCreate:   "package_verification__create",
		&queryPackageVerificationList:     "package_verification__list",
		&queryPackageVersionCreate:        "package_version__create",
		&queryPackageVersionGetByID:       "package_version__get_by_id",
		&queryPackageVersionInsertIgnore:  "package_version__insert_ignore",
		&queryPackageVersionList:          "package_version__list",
		&queryPackageVersionVerification:  "package_version__set_verification",
		&queryPackageVersionUpdateByLabel: "package_version__update_by_package_label",
		&queryPricingMetadataCreate:       "pricing_metadata__create",
		&queryPricingMetadataGetByPackage: "pricing_metadata__get_by_package",
		&queryPricingMetadataUpdate:       "pricing_metadata__update",
	} {
		*target = mustLoadQuery(name)
	}
}

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
