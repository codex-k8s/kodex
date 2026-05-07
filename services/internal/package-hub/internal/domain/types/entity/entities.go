// Package entity contains package-hub domain entities.
package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type VersionedBase struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PackageSource struct {
	VersionedBase
	OrganizationID     *uuid.UUID
	Slug               string
	DisplayName        string
	Kind               enum.PackageSourceKind
	RepositoryRef      string
	CatalogEndpointRef string
	Status             enum.PackageSourceStatus
	LastSyncAt         *time.Time
	LastError          string
}

type PackageEntry struct {
	VersionedBase
	SourceID         *uuid.UUID
	Slug             string
	Kind             enum.PackageKind
	PublisherRef     string
	DisplayName      []value.LocalizedText
	Description      []value.LocalizedText
	IconObjectURI    string
	CommercialStatus enum.PackageCommercialStatus
	TrustStatus      enum.PackageTrustStatus
	Status           enum.PackageStatus
}

type PackageVersion struct {
	ID                 uuid.UUID
	PackageID          uuid.UUID
	VersionLabel       string
	SourceRef          value.SourceRef
	ManifestDigest     string
	VerificationStatus enum.PackageVerificationStatus
	ReleaseStatus      enum.PackageReleaseStatus
	Revision           int64
	PublishedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PackageManifestSnapshot struct {
	ID               uuid.UUID
	PackageVersionID uuid.UUID
	SchemaVersion    int32
	Payload          []byte
	ValidationStatus enum.PackageManifestValidationStatus
	ValidationErrors []byte
	CreatedAt        time.Time
}

type PackagePricingMetadata struct {
	ID           uuid.UUID
	PackageID    uuid.UUID
	Kind         enum.PackagePricingKind
	Currency     string
	PricePayload []byte
	Version      int64
	UpdatedAt    time.Time
}
