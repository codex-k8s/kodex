package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
)

const (
	packageEventSourceConnected     = packageevents.EventSourceConnected
	packageEventSourceDisabled      = packageevents.EventSourceDisabled
	packageEventSourceUpdated       = packageevents.EventSourceUpdated
	packageEventVerificationUpdated = packageevents.EventVerificationUpdated
	packageAggregateSource          = packageevents.AggregatePackageSource
	packageAggregateVersion         = packageevents.AggregatePackageVersion
	packageOperationSourceConnect   = "domain.Service.ConnectPackageSource"
	packageOperationSourceDisable   = "domain.Service.DisablePackageSource"
	packageOperationSourceUpdate    = "domain.Service.UpdatePackageSource"
	packageOperationVerify          = "domain.Service.SetPackageVerification"
	packageActionSourceConnect      = accesscatalog.ActionPackageSourceConnect
	packageActionSourceDisable      = accesscatalog.ActionPackageSourceDisable
	packageActionSourceRead         = accesscatalog.ActionPackageSourceRead
	packageActionSourceUpdate       = accesscatalog.ActionPackageSourceUpdate
	packageActionCatalogRead        = accesscatalog.ActionPackageCatalogRead
	packageActionManifestRead       = accesscatalog.ActionPackageManifestRead
	packageActionVerify             = accesscatalog.ActionPackageVerify
	packageResourceSource           = accesscatalog.ResourcePackageSource
	packageResourcePackage          = accesscatalog.ResourcePackage
	packageResourceVersion          = accesscatalog.ResourcePackageVersion
	packageResourceManifest         = accesscatalog.ResourcePackageManifest
	packageScopeGlobal              = accesscatalog.ScopeGlobal
	packageScopeOrganization        = accesscatalog.ScopeOrganization
)

type resourceRef struct {
	Type      string
	ID        string
	ScopeType string
	ScopeID   string
}
