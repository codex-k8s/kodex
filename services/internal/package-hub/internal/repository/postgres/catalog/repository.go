// Package catalog implements the PostgreSQL repository for package-hub catalog data.
package catalog

import (
	"context"
	"embed"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	catalogrepo "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/repository/catalog"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

// SQLFiles contains named SQL queries for package-hub catalog repository.
//
//go:embed sql/*.sql
var SQLFiles embed.FS

var _ catalogrepo.Repository = (*Repository)(nil)

type execQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db execQuerier
}

const (
	operationCreateManifestSnapshot = "domain.Repository.CreateManifestSnapshot"
	operationCreatePackage          = "domain.Repository.CreatePackage"
	operationCreatePackageSource    = "domain.Repository.CreatePackageSource"
	operationCreatePackageVersion   = "domain.Repository.CreatePackageVersion"
	operationCreatePricingMetadata  = "domain.Repository.CreatePricingMetadata"
	operationGetLatestManifest      = "domain.Repository.GetLatestManifestSnapshot"
	operationGetPackage             = "domain.Repository.GetPackage"
	operationGetPackageSource       = "domain.Repository.GetPackageSource"
	operationGetPackageVersion      = "domain.Repository.GetPackageVersion"
	operationGetPricingMetadata     = "domain.Repository.GetPricingMetadata"
	operationListPackageSources     = "domain.Repository.ListPackageSources"
	operationListPackageVersions    = "domain.Repository.ListPackageVersions"
	operationListPackages           = "domain.Repository.ListPackages"
	operationUpdatePricingMetadata  = "domain.Repository.UpdatePricingMetadata"
)

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreatePackageSource(ctx context.Context, source entity.PackageSource) error {
	_, err := r.db.Exec(ctx, queryPackageSourceCreate, packageSourceArgs(source))
	return wrapError(operationCreatePackageSource, err)
}

func (r *Repository) GetPackageSource(ctx context.Context, id uuid.UUID) (entity.PackageSource, error) {
	return queryOne(ctx, r.db, operationGetPackageSource, queryPackageSourceGetByID, pgx.NamedArgs{"id": id}, scanPackageSource)
}

func (r *Repository) ListPackageSources(ctx context.Context, filter query.PackageSourceFilter) ([]entity.PackageSource, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackageSources, queryPackageSourceList, packageSourceFilterArgs(filter), scanPackageSource)
}

func (r *Repository) CreatePackage(ctx context.Context, entry entity.PackageEntry) error {
	_, err := r.db.Exec(ctx, queryPackageCreate, packageArgs(entry))
	return wrapError(operationCreatePackage, err)
}

func (r *Repository) GetPackage(ctx context.Context, id uuid.UUID) (entity.PackageEntry, error) {
	return queryOne(ctx, r.db, operationGetPackage, queryPackageGetByID, pgx.NamedArgs{"id": id}, scanPackage)
}

func (r *Repository) ListPackages(ctx context.Context, filter query.PackageFilter) ([]entity.PackageEntry, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackages, queryPackageList, packageFilterArgs(filter), scanPackage)
}

func (r *Repository) CreatePackageVersion(ctx context.Context, version entity.PackageVersion) error {
	_, err := r.db.Exec(ctx, queryPackageVersionCreate, packageVersionArgs(version))
	return wrapError(operationCreatePackageVersion, err)
}

func (r *Repository) GetPackageVersion(ctx context.Context, id uuid.UUID) (entity.PackageVersion, error) {
	return queryOne(ctx, r.db, operationGetPackageVersion, queryPackageVersionGetByID, pgx.NamedArgs{"id": id}, scanPackageVersion)
}

func (r *Repository) ListPackageVersions(ctx context.Context, filter query.PackageVersionFilter) ([]entity.PackageVersion, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackageVersions, queryPackageVersionList, packageVersionFilterArgs(filter), scanPackageVersion)
}

func (r *Repository) CreateManifestSnapshot(ctx context.Context, snapshot entity.PackageManifestSnapshot) error {
	_, err := r.db.Exec(ctx, queryManifestSnapshotCreate, manifestSnapshotArgs(snapshot))
	return wrapError(operationCreateManifestSnapshot, err)
}

func (r *Repository) GetLatestManifestSnapshot(ctx context.Context, packageVersionID uuid.UUID) (entity.PackageManifestSnapshot, error) {
	return queryOne(ctx, r.db, operationGetLatestManifest, queryManifestSnapshotGetLatest, pgx.NamedArgs{"package_version_id": packageVersionID}, scanManifestSnapshot)
}

func (r *Repository) CreatePricingMetadata(ctx context.Context, metadata entity.PackagePricingMetadata) error {
	err := postgreslib.RunMutation(ctx, r.db, errs.ErrConflict, postgreslib.Mutation{
		Query:           queryPricingMetadataCreate,
		Args:            pricingMetadataArgs(metadata),
		RequireAffected: true,
	})
	return wrapError(operationCreatePricingMetadata, err)
}

func (r *Repository) UpdatePricingMetadata(ctx context.Context, metadata entity.PackagePricingMetadata, previousVersion int64) error {
	err := postgreslib.RunMutation(ctx, r.db, errs.ErrConflict, postgreslib.Mutation{
		Query:           queryPricingMetadataUpdate,
		Args:            pricingMetadataUpdateArgs(metadata, previousVersion),
		RequireAffected: true,
	})
	return wrapError(operationUpdatePricingMetadata, err)
}

func (r *Repository) GetPricingMetadata(ctx context.Context, packageID uuid.UUID) (entity.PackagePricingMetadata, error) {
	return queryOne(ctx, r.db, operationGetPricingMetadata, queryPricingMetadataGetByPackage, pgx.NamedArgs{"package_id": packageID}, scanPricingMetadata)
}

func queryOne[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) (T, error) {
	value, err := scan(db.QueryRow(ctx, queryText, args))
	if err != nil {
		var zero T
		return zero, wrapError(operation, err)
	}
	return value, nil
}

func queryPage[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pageQueryArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, value.PageResult, error) {
	rows, err := db.Query(ctx, queryText, args.NamedArgs)
	if err != nil {
		return nil, value.PageResult{}, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	if err != nil {
		return nil, value.PageResult{}, wrapError(operation, err)
	}
	return trimPage(items, args.PageSize, args.Offset), pageResult(items, args.PageSize, args.Offset), nil
}
