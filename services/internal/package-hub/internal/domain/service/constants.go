package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
)

const (
	packageEventVerificationUpdated = packageevents.EventVerificationUpdated
	packageAggregateVersion         = packageevents.AggregatePackageVersion
	packageOperationVerify          = "domain.Service.SetPackageVerification"
	packageActionSourceRead         = accesscatalog.ActionPackageSourceRead
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
