package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
)

const (
	packageEventSourceConnected         = packageevents.EventSourceConnected
	packageEventSourceDisabled          = packageevents.EventSourceDisabled
	packageEventSourceUpdated           = packageevents.EventSourceUpdated
	packageEventCatalogSynced           = packageevents.EventCatalogSynced
	packageEventPackageDiscovered       = packageevents.EventPackageDiscovered
	packageEventPackageUpdated          = packageevents.EventPackageUpdated
	packageEventVersionDiscovered       = packageevents.EventVersionDiscovered
	packageEventVersionUpdated          = packageevents.EventVersionUpdated
	packageEventVerificationUpdated     = packageevents.EventVerificationUpdated
	packageEventInstallationRequested   = packageevents.EventInstallationRequested
	packageEventInstallationActivated   = packageevents.EventInstallationActivated
	packageEventInstallationUpdated     = packageevents.EventInstallationUpdated
	packageEventInstallationDisabled    = packageevents.EventInstallationDisabled
	packageEventInstallationUninstalled = packageevents.EventInstallationUninstalled
	packageEventSecretSchemaUpdated     = packageevents.EventSecretSchemaUpdated
	packageAggregatePackage             = packageevents.AggregatePackage
	packageAggregateSource              = packageevents.AggregatePackageSource
	packageAggregateVersion             = packageevents.AggregatePackageVersion
	packageAggregateInstallation        = packageevents.AggregatePackageInstallation
	packageOperationCatalogSync         = "domain.Service.SyncAvailablePackages"
	packageOperationInstall             = "domain.Service.RequestPackageInstallation"
	packageOperationInstallationUpdate  = "domain.Service.UpdatePackageInstallation"
	packageOperationInstallationDisable = "domain.Service.DisablePackageInstallation"
	packageOperationUninstall           = "domain.Service.UninstallPackage"
	packageOperationSourceConnect       = "domain.Service.ConnectPackageSource"
	packageOperationSourceDisable       = "domain.Service.DisablePackageSource"
	packageOperationSourceUpdate        = "domain.Service.UpdatePackageSource"
	packageOperationVerify              = "domain.Service.SetPackageVerification"
	packageActionCatalogSync            = accesscatalog.ActionPackageCatalogSync
	packageActionSourceConnect          = accesscatalog.ActionPackageSourceConnect
	packageActionSourceDisable          = accesscatalog.ActionPackageSourceDisable
	packageActionSourceRead             = accesscatalog.ActionPackageSourceRead
	packageActionSourceUpdate           = accesscatalog.ActionPackageSourceUpdate
	packageActionCatalogRead            = accesscatalog.ActionPackageCatalogRead
	packageActionManifestRead           = accesscatalog.ActionPackageManifestRead
	packageActionInstall                = accesscatalog.ActionPackageInstall
	packageActionInstallationUpdate     = accesscatalog.ActionPackageInstallationUpdate
	packageActionInstallationDisable    = accesscatalog.ActionPackageInstallationDisable
	packageActionUninstall              = accesscatalog.ActionPackageUninstall
	packageActionInstallationRead       = accesscatalog.ActionPackageInstallationRead
	packageActionSecretRead             = accesscatalog.ActionPackageSecretRead
	packageActionVerify                 = accesscatalog.ActionPackageVerify
	packageResourceSource               = accesscatalog.ResourcePackageSource
	packageResourceCatalog              = accesscatalog.ResourcePackageCatalog
	packageResourcePackage              = accesscatalog.ResourcePackage
	packageResourceVersion              = accesscatalog.ResourcePackageVersion
	packageResourceManifest             = accesscatalog.ResourcePackageManifest
	packageResourceInstallation         = accesscatalog.ResourcePackageInstallation
	packageResourceSecretSchema         = accesscatalog.ResourcePackageSecretSchema
	packageScopeGlobal                  = accesscatalog.ScopeGlobal
	packageScopeOrganization            = accesscatalog.ScopeOrganization
	packageScopeProject                 = accesscatalog.ScopeProject
	packageScopeRepository              = accesscatalog.ScopeRepository
)

type resourceRef struct {
	Type      string
	ID        string
	ScopeType string
	ScopeID   string
}
