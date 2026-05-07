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

type PackageInstallationFilter struct {
	Scope               *value.ScopeRef
	PackageID           *uuid.UUID
	PackageKind         *enum.PackageKind
	InstallationStatus  *enum.PackageInstallationStatus
	SecretBindingStatus *enum.PackageSecretBindingStatus
	Page                value.PageRequest
}

type PackageVerificationFilter struct {
	PackageVersionID   uuid.UUID
	VerificationStatus *enum.PackageVerificationStatus
	Page               value.PageRequest
}

type CommandIdentity struct {
	CommandID      *uuid.UUID
	IdempotencyKey string
	Operation      string
}
