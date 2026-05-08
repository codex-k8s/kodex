// Package catalog defines package-hub catalog persistence ports.
package catalog

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type Repository interface {
	CreatePackageSource(ctx context.Context, source entity.PackageSource) error
	CreatePackageSourceWithResult(ctx context.Context, source entity.PackageSource, result entity.CommandResult, event entity.OutboxEvent) error
	GetPackageSource(ctx context.Context, id uuid.UUID) (entity.PackageSource, error)
	ListPackageSources(ctx context.Context, filter query.PackageSourceFilter) ([]entity.PackageSource, value.PageResult, error)
	UpdatePackageSourceWithResult(ctx context.Context, source entity.PackageSource, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error
	SyncAvailableCatalog(ctx context.Context, plan CatalogSyncPlan) (CatalogSyncOutcome, error)
	CreatePackage(ctx context.Context, entry entity.PackageEntry) error
	GetPackage(ctx context.Context, id uuid.UUID) (entity.PackageEntry, error)
	ListPackages(ctx context.Context, filter query.PackageFilter) ([]entity.PackageEntry, value.PageResult, error)
	CreatePackageVersion(ctx context.Context, version entity.PackageVersion) error
	GetPackageVersion(ctx context.Context, id uuid.UUID) (entity.PackageVersion, error)
	ListPackageVersions(ctx context.Context, filter query.PackageVersionFilter) ([]entity.PackageVersion, value.PageResult, error)
	CreateManifestSnapshot(ctx context.Context, snapshot entity.PackageManifestSnapshot) error
	GetLatestManifestSnapshot(ctx context.Context, packageVersionID uuid.UUID) (entity.PackageManifestSnapshot, error)
	CreatePricingMetadata(ctx context.Context, metadata entity.PackagePricingMetadata) error
	UpdatePricingMetadata(ctx context.Context, metadata entity.PackagePricingMetadata, previousVersion int64) error
	GetPricingMetadata(ctx context.Context, packageID uuid.UUID) (entity.PackagePricingMetadata, error)
	CreatePackageInstallation(ctx context.Context, installation entity.PackageInstallation) error
	CreatePackageInstallationWithResult(ctx context.Context, installation entity.PackageInstallation, result entity.CommandResult, event entity.OutboxEvent) error
	UpdatePackageInstallation(ctx context.Context, installation entity.PackageInstallation, previousVersion int64) error
	GetPackageInstallation(ctx context.Context, id uuid.UUID) (entity.PackageInstallation, error)
	ListPackageInstallations(ctx context.Context, filter query.PackageInstallationFilter) ([]entity.PackageInstallation, value.PageResult, error)
	CreatePackageSecretSchema(ctx context.Context, schema entity.PackageSecretSchema) error
	GetLatestPackageSecretSchema(ctx context.Context, packageVersionID uuid.UUID) (entity.PackageSecretSchema, error)
	SetPackageVerification(ctx context.Context, version entity.PackageVersion, previousRevision int64, verification entity.PackageVerification, result entity.CommandResult, event entity.OutboxEvent) error
	ListPackageVerifications(ctx context.Context, filter query.PackageVerificationFilter) ([]entity.PackageVerification, value.PageResult, error)
	GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error)
	ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error)
	MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error
	MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error
	MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() uuid.UUID
}

type CatalogSyncPlan struct {
	Source                entity.PackageSource
	PreviousSourceVersion int64
	Items                 []CatalogSyncItem
	Result                entity.CommandResult
	BuildEvents           CatalogSyncEventBuilder
}

type CatalogSyncEventBuilder func(CatalogSyncOutcome) ([]entity.OutboxEvent, error)

type CatalogSyncItem struct {
	Entry    entity.PackageEntry
	Versions []CatalogSyncVersionPlan
}

type CatalogSyncVersionPlan struct {
	Version  entity.PackageVersion
	Manifest entity.PackageManifestSnapshot
}

type CatalogSyncOutcome struct {
	Source        entity.PackageSource
	Packages      []CatalogSyncPackage
	Versions      []CatalogSyncVersion
	ManifestCount int
}

type CatalogSyncPackage struct {
	Entry    entity.PackageEntry
	Inserted bool
	Changed  bool
}

type CatalogSyncVersion struct {
	Version  entity.PackageVersion
	Inserted bool
	Changed  bool
}
