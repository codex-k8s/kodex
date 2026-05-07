// Package query contains package-hub repository filters.
package query

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type PackageSourceFilter struct {
	OrganizationID *uuid.UUID
	Kind           *enum.PackageSourceKind
	Status         *enum.PackageSourceStatus
	Page           value.PageRequest
}

type PackageFilter struct {
	SourceID         *uuid.UUID
	Kind             *enum.PackageKind
	Status           *enum.PackageStatus
	CommercialStatus *enum.PackageCommercialStatus
	TrustStatus      *enum.PackageTrustStatus
	Query            string
	Page             value.PageRequest
}

type PackageVersionFilter struct {
	PackageID          uuid.UUID
	VerificationStatus *enum.PackageVerificationStatus
	ReleaseStatus      *enum.PackageReleaseStatus
	Page               value.PageRequest
}
