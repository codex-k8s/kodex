// Package enum contains package-hub closed domain classifiers.
package enum

type PackageSourceKind string

const (
	PackageSourceKindBuiltIn          PackageSourceKind = "built_in"
	PackageSourceKindStorePackage     PackageSourceKind = "store_package"
	PackageSourceKindCustomRepository PackageSourceKind = "custom_repository"
	PackageSourceKindProxy            PackageSourceKind = "proxy"
)

type PackageSourceStatus string

const (
	PackageSourceStatusActive     PackageSourceStatus = "active"
	PackageSourceStatusDisabled   PackageSourceStatus = "disabled"
	PackageSourceStatusBlocked    PackageSourceStatus = "blocked"
	PackageSourceStatusSyncFailed PackageSourceStatus = "sync_failed"
)

type PackageKind string

const (
	PackageKindPlugin          PackageKind = "plugin"
	PackageKindGuidance        PackageKind = "guidance"
	PackageKindStore           PackageKind = "store"
	PackageKindPlatformContent PackageKind = "platform_content"
)

type PackageCommercialStatus string

const (
	PackageCommercialStatusFree       PackageCommercialStatus = "free"
	PackageCommercialStatusPaid       PackageCommercialStatus = "paid"
	PackageCommercialStatusRestricted PackageCommercialStatus = "restricted"
	PackageCommercialStatusUnknown    PackageCommercialStatus = "unknown"
)

type PackageTrustStatus string

const (
	PackageTrustStatusBuiltIn    PackageTrustStatus = "built_in"
	PackageTrustStatusVerified   PackageTrustStatus = "verified"
	PackageTrustStatusUnverified PackageTrustStatus = "unverified"
	PackageTrustStatusBlocked    PackageTrustStatus = "blocked"
)

type PackageStatus string

const (
	PackageStatusAvailable PackageStatus = "available"
	PackageStatusHidden    PackageStatus = "hidden"
	PackageStatusRevoked   PackageStatus = "revoked"
	PackageStatusBlocked   PackageStatus = "blocked"
)

type PackageVersionSourceRefKind string

const (
	PackageVersionSourceRefKindGitTag    PackageVersionSourceRefKind = "git_tag"
	PackageVersionSourceRefKindGitCommit PackageVersionSourceRefKind = "git_commit"
	PackageVersionSourceRefKindGitlink   PackageVersionSourceRefKind = "gitlink"
	PackageVersionSourceRefKindProxyRef  PackageVersionSourceRefKind = "proxy_ref"
)

type PackageVerificationStatus string

const (
	PackageVerificationStatusVerified   PackageVerificationStatus = "verified"
	PackageVerificationStatusUnverified PackageVerificationStatus = "unverified"
	PackageVerificationStatusRejected   PackageVerificationStatus = "rejected"
	PackageVerificationStatusRevoked    PackageVerificationStatus = "revoked"
)

type PackageReleaseStatus string

const (
	PackageReleaseStatusActive     PackageReleaseStatus = "active"
	PackageReleaseStatusDeprecated PackageReleaseStatus = "deprecated"
	PackageReleaseStatusRevoked    PackageReleaseStatus = "revoked"
	PackageReleaseStatusBlocked    PackageReleaseStatus = "blocked"
)

type PackageManifestValidationStatus string

const (
	PackageManifestValidationStatusValid   PackageManifestValidationStatus = "valid"
	PackageManifestValidationStatusInvalid PackageManifestValidationStatus = "invalid"
	PackageManifestValidationStatusWarning PackageManifestValidationStatus = "warning"
)

type PackagePricingKind string

const (
	PackagePricingKindFree         PackagePricingKind = "free"
	PackagePricingKindPaid         PackagePricingKind = "paid"
	PackagePricingKindSubscription PackagePricingKind = "subscription"
	PackagePricingKindUsageBased   PackagePricingKind = "usage_based"
	PackagePricingKindRestricted   PackagePricingKind = "restricted"
)

type PackageInstallationScopeType string

const (
	PackageInstallationScopeTypePlatform     PackageInstallationScopeType = "platform"
	PackageInstallationScopeTypeOrganization PackageInstallationScopeType = "organization"
	PackageInstallationScopeTypeProject      PackageInstallationScopeType = "project"
	PackageInstallationScopeTypeRepository   PackageInstallationScopeType = "repository"
)

type PackageInstallationStatus string

const (
	PackageInstallationStatusRequested   PackageInstallationStatus = "requested"
	PackageInstallationStatusActive      PackageInstallationStatus = "active"
	PackageInstallationStatusDisabled    PackageInstallationStatus = "disabled"
	PackageInstallationStatusFailed      PackageInstallationStatus = "failed"
	PackageInstallationStatusUninstalled PackageInstallationStatus = "uninstalled"
)

type PackageDesiredState string

const (
	PackageDesiredStatePresent   PackageDesiredState = "present"
	PackageDesiredStateAbsent    PackageDesiredState = "absent"
	PackageDesiredStateSuspended PackageDesiredState = "suspended"
)

type PackageSecretBindingStatus string

const (
	PackageSecretBindingStatusNotRequired PackageSecretBindingStatus = "not_required"
	PackageSecretBindingStatusMissing     PackageSecretBindingStatus = "missing"
	PackageSecretBindingStatusComplete    PackageSecretBindingStatus = "complete"
	PackageSecretBindingStatusInvalid     PackageSecretBindingStatus = "invalid"
	PackageSecretBindingStatusPartial     PackageSecretBindingStatus = "partial"
	PackageSecretBindingStatusCheckFailed PackageSecretBindingStatus = "check_failed"
)

type PackageHealthStatus string

const (
	PackageHealthStatusUnknown  PackageHealthStatus = "unknown"
	PackageHealthStatusHealthy  PackageHealthStatus = "healthy"
	PackageHealthStatusDegraded PackageHealthStatus = "degraded"
	PackageHealthStatusFailed   PackageHealthStatus = "failed"
)

type PackageSecretFieldKind string

const (
	PackageSecretFieldKindString   PackageSecretFieldKind = "string"
	PackageSecretFieldKindPassword PackageSecretFieldKind = "password"
	PackageSecretFieldKindToken    PackageSecretFieldKind = "token"
	PackageSecretFieldKindJSON     PackageSecretFieldKind = "json"
	PackageSecretFieldKindURL      PackageSecretFieldKind = "url"
)

type PackageInstallationSecretRefStatus string

const (
	PackageInstallationSecretRefStatusConfigured PackageInstallationSecretRefStatus = "configured"
	PackageInstallationSecretRefStatusMissing    PackageInstallationSecretRefStatus = "missing"
	PackageInstallationSecretRefStatusInvalid    PackageInstallationSecretRefStatus = "invalid"
	PackageInstallationSecretRefStatusDisabled   PackageInstallationSecretRefStatus = "disabled"
)

type CommandAggregateType string

const (
	CommandAggregateTypePackageSource  CommandAggregateType = "package_source"
	CommandAggregateTypePackage        CommandAggregateType = "package"
	CommandAggregateTypePackageVersion CommandAggregateType = "package_version"
	CommandAggregateTypeInstallation   CommandAggregateType = "installation"
	CommandAggregateTypeVerification   CommandAggregateType = "verification"
)
