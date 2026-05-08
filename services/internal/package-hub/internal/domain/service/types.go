package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type ListPackageSourcesInput struct {
	OrganizationID *uuid.UUID
	Kind           *enum.PackageSourceKind
	Status         *enum.PackageSourceStatus
	Page           value.PageRequest
	Meta           value.QueryMeta
}

type ListPackageSourcesResult struct {
	Sources []entity.PackageSource
	Page    value.PageResult
}

type ConnectPackageSourceInput struct {
	OrganizationID     *uuid.UUID
	Slug               string
	DisplayName        string
	Kind               enum.PackageSourceKind
	RepositoryRef      string
	CatalogEndpointRef string
	Meta               value.CommandMeta
}

type UpdatePackageSourceInput struct {
	SourceID           uuid.UUID
	DisplayName        *string
	RepositoryRef      *string
	CatalogEndpointRef *string
	Status             *enum.PackageSourceStatus
	Meta               value.CommandMeta
}

type DisablePackageSourceInput struct {
	SourceID uuid.UUID
	Meta     value.CommandMeta
}

type ListPackagesInput struct {
	SourceID         *uuid.UUID
	Kind             *enum.PackageKind
	Status           *enum.PackageStatus
	CommercialStatus *enum.PackageCommercialStatus
	TrustStatus      *enum.PackageTrustStatus
	Query            string
	Page             value.PageRequest
	Meta             value.QueryMeta
}

type ListPackagesResult struct {
	Packages []entity.PackageEntry
	Page     value.PageResult
}

type CatalogSnapshot struct {
	Packages   []CatalogPackageSnapshot
	ObservedAt *time.Time
}

type CatalogPackageSnapshot struct {
	Slug             string
	Kind             enum.PackageKind
	PublisherRef     string
	DisplayName      []value.LocalizedText
	Description      []value.LocalizedText
	IconObjectURI    string
	CommercialStatus enum.PackageCommercialStatus
	TrustStatus      enum.PackageTrustStatus
	Status           enum.PackageStatus
	Versions         []CatalogVersionSnapshot
}

type CatalogVersionSnapshot struct {
	VersionLabel       string
	SourceRef          value.SourceRef
	ManifestDigest     string
	ManifestSchema     int32
	ManifestPayload    []byte
	VerificationStatus enum.PackageVerificationStatus
	ReleaseStatus      enum.PackageReleaseStatus
	PublishedAt        *time.Time
}

type SyncAvailablePackagesInput struct {
	SourceID uuid.UUID
	Snapshot CatalogSnapshot
	Meta     value.CommandMeta
}

type SyncAvailablePackagesResult struct {
	Source       entity.PackageSource
	PackageCount int64
	VersionCount int64
	SyncedAt     time.Time
}

type ListPackageVersionsInput struct {
	PackageID          uuid.UUID
	VerificationStatus *enum.PackageVerificationStatus
	ReleaseStatus      *enum.PackageReleaseStatus
	Page               value.PageRequest
	Meta               value.QueryMeta
}

type ListPackageVersionsResult struct {
	Versions []entity.PackageVersion
	Page     value.PageResult
}

type RequestPackageInstallationInput struct {
	PackageID        uuid.UUID
	PackageVersionID uuid.UUID
	Scope            value.ScopeRef
	DesiredState     *enum.PackageDesiredState
	Meta             value.CommandMeta
}

type UpdatePackageInstallationInput struct {
	InstallationID     uuid.UUID
	PackageVersionID   *uuid.UUID
	DesiredState       *enum.PackageDesiredState
	InstallationStatus *enum.PackageInstallationStatus
	Meta               value.CommandMeta
}

type DisablePackageInstallationInput struct {
	InstallationID uuid.UUID
	Meta           value.CommandMeta
}

type UninstallPackageInput struct {
	InstallationID uuid.UUID
	Meta           value.CommandMeta
}

type ListPackageInstallationsInput struct {
	Scope               *value.ScopeRef
	PackageID           *uuid.UUID
	PackageKind         *enum.PackageKind
	InstallationStatus  *enum.PackageInstallationStatus
	SecretBindingStatus *enum.PackageSecretBindingStatus
	Page                value.PageRequest
	Meta                value.QueryMeta
}

type ListPackageInstallationsResult struct {
	Installations []entity.PackageInstallation
	Page          value.PageResult
}

type SetPackageVerificationInput struct {
	PackageVersionID   uuid.UUID
	VerificationStatus enum.PackageVerificationStatus
	VerificationNotes  string
	ReleaseStatus      *enum.PackageReleaseStatus
	Meta               value.CommandMeta
}

type SetPackageVerificationResult struct {
	Verification entity.PackageVerification
	Version      entity.PackageVersion
}
