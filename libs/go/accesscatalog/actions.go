// Package accesscatalog contains shared system access action keys.
package accesscatalog

// ActionDescriptor describes one code-owned system access action.
type ActionDescriptor struct {
	Key          string
	ResourceType string
}

const (
	ResourceProject             = "project"
	ResourceRepository          = "repository"
	ResourceServicesPolicy      = "services_policy"
	ResourcePolicyOverride      = "policy_override"
	ResourceDocumentationSource = "documentation_source"
	ResourceBranchRules         = "branch_rules"
	ResourceReleasePolicy       = "release_policy"
	ResourceReleaseLine         = "release_line"
	ResourcePlacementPolicy     = "placement_policy"
	ResourcePackageSource       = "package_source"
	ResourcePackageCatalog      = "package_catalog"
	ResourcePackage             = "package"
	ResourcePackageVersion      = "package_version"
	ResourcePackageManifest     = "package_manifest"
	ResourcePackageInstallation = "package_installation"
	ResourcePackageSecretSchema = "package_secret_schema"
)

const (
	ActionProjectCreate               = "project.create"
	ActionProjectUpdate               = "project.update"
	ActionProjectRead                 = "project.read"
	ActionProjectList                 = "project.list"
	ActionRepositoryAttach            = "repository.attach"
	ActionRepositoryUpdate            = "repository.update"
	ActionRepositoryDetach            = "repository.detach"
	ActionRepositoryRead              = "repository.read"
	ActionRepositoryList              = "repository.list"
	ActionProjectPolicyImport         = "project.policy.import"
	ActionProjectPolicyRead           = "project.policy.read"
	ActionProjectPolicyPropose        = "project.policy.propose"
	ActionProjectPolicyOverride       = "project.policy.override"
	ActionProjectPolicyOverrideRead   = "project.policy.override.read"
	ActionProjectPolicyOverrideCancel = "project.policy.override.cancel"
	ActionProjectDocsUpdate           = "project.docs.update"
	ActionProjectDocsRead             = "project.docs.read"
	ActionProjectWorkspaceRead        = "project.workspace.read"
	ActionProjectBranchRulesUpdate    = "project.branch_rules.update"
	ActionProjectBranchRulesRead      = "project.branch_rules.read"
	ActionProjectReleasePolicyUpdate  = "project.release_policy.update"
	ActionProjectReleasePolicyRead    = "project.release_policy.read"
	ActionProjectReleaseLineUpdate    = "project.release_line.update"
	ActionProjectReleaseLineRead      = "project.release_line.read"
	ActionProjectPlacementUpdate      = "project.placement_policy.update"
	ActionProjectPlacementRead        = "project.placement_policy.read"
	ActionPackageSourceConnect        = "package.source.connect"
	ActionPackageSourceUpdate         = "package.source.update"
	ActionPackageSourceDisable        = "package.source.disable"
	ActionPackageSourceRead           = "package.source.read"
	ActionPackageCatalogSync          = "package.catalog.sync"
	ActionPackageCatalogRead          = "package.catalog.read"
	ActionPackageManifestRead         = "package.manifest.read"
	ActionPackageInstall              = "package.install"
	ActionPackageInstallationUpdate   = "package.installation.update"
	ActionPackageInstallationDisable  = "package.installation.disable"
	ActionPackageUninstall            = "package.uninstall"
	ActionPackageInstallationRead     = "package.installation.read"
	ActionPackageSecretRead           = "package.secret.read"
	ActionPackageVerify               = "package.verify"
)

// ProjectCatalogActions returns system actions owned by the projects-and-repositories domain.
func ProjectCatalogActions() []ActionDescriptor {
	return []ActionDescriptor{
		{Key: ActionProjectCreate, ResourceType: ResourceProject},
		{Key: ActionProjectUpdate, ResourceType: ResourceProject},
		{Key: ActionProjectRead, ResourceType: ResourceProject},
		{Key: ActionProjectList, ResourceType: ResourceProject},
		{Key: ActionRepositoryAttach, ResourceType: ResourceRepository},
		{Key: ActionRepositoryUpdate, ResourceType: ResourceRepository},
		{Key: ActionRepositoryDetach, ResourceType: ResourceRepository},
		{Key: ActionRepositoryRead, ResourceType: ResourceRepository},
		{Key: ActionRepositoryList, ResourceType: ResourceRepository},
		{Key: ActionProjectPolicyImport, ResourceType: ResourceServicesPolicy},
		{Key: ActionProjectPolicyRead, ResourceType: ResourceServicesPolicy},
		{Key: ActionProjectPolicyPropose, ResourceType: ResourceServicesPolicy},
		{Key: ActionProjectPolicyOverride, ResourceType: ResourcePolicyOverride},
		{Key: ActionProjectPolicyOverrideRead, ResourceType: ResourcePolicyOverride},
		{Key: ActionProjectPolicyOverrideCancel, ResourceType: ResourcePolicyOverride},
		{Key: ActionProjectDocsUpdate, ResourceType: ResourceDocumentationSource},
		{Key: ActionProjectDocsRead, ResourceType: ResourceDocumentationSource},
		{Key: ActionProjectWorkspaceRead, ResourceType: ResourceProject},
		{Key: ActionProjectBranchRulesUpdate, ResourceType: ResourceBranchRules},
		{Key: ActionProjectBranchRulesRead, ResourceType: ResourceBranchRules},
		{Key: ActionProjectReleasePolicyUpdate, ResourceType: ResourceReleasePolicy},
		{Key: ActionProjectReleasePolicyRead, ResourceType: ResourceReleasePolicy},
		{Key: ActionProjectReleaseLineUpdate, ResourceType: ResourceReleaseLine},
		{Key: ActionProjectReleaseLineRead, ResourceType: ResourceReleaseLine},
		{Key: ActionProjectPlacementUpdate, ResourceType: ResourcePlacementPolicy},
		{Key: ActionProjectPlacementRead, ResourceType: ResourcePlacementPolicy},
	}
}

// PackageHubActions returns system actions owned by the package platform domain.
func PackageHubActions() []ActionDescriptor {
	return []ActionDescriptor{
		{Key: ActionPackageSourceConnect, ResourceType: ResourcePackageSource},
		{Key: ActionPackageSourceUpdate, ResourceType: ResourcePackageSource},
		{Key: ActionPackageSourceDisable, ResourceType: ResourcePackageSource},
		{Key: ActionPackageSourceRead, ResourceType: ResourcePackageSource},
		{Key: ActionPackageCatalogSync, ResourceType: ResourcePackageCatalog},
		{Key: ActionPackageCatalogRead, ResourceType: ResourcePackage},
		{Key: ActionPackageManifestRead, ResourceType: ResourcePackageManifest},
		{Key: ActionPackageInstall, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationUpdate, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationDisable, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageUninstall, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageInstallationRead, ResourceType: ResourcePackageInstallation},
		{Key: ActionPackageSecretRead, ResourceType: ResourcePackageSecretSchema},
		{Key: ActionPackageVerify, ResourceType: ResourcePackageVersion},
	}
}
