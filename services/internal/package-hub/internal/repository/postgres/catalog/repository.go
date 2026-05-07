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
	operationCreateCommandResult       = "domain.Repository.CreateCommandResult"
	operationCreateManifestSnapshot    = "domain.Repository.CreateManifestSnapshot"
	operationCreatePackage             = "domain.Repository.CreatePackage"
	operationCreatePackageInstallation = "domain.Repository.CreatePackageInstallation"
	operationCreatePackageSecretSchema = "domain.Repository.CreatePackageSecretSchema"
	operationCreatePackageSource       = "domain.Repository.CreatePackageSource"
	operationCreatePackageVerification = "domain.Repository.CreatePackageVerification"
	operationCreatePackageVersion      = "domain.Repository.CreatePackageVersion"
	operationCreatePricingMetadata     = "domain.Repository.CreatePricingMetadata"
	operationGetCommandResult          = "domain.Repository.GetCommandResult"
	operationGetLatestManifest         = "domain.Repository.GetLatestManifestSnapshot"
	operationGetLatestSecretSchema     = "domain.Repository.GetLatestPackageSecretSchema"
	operationGetPackage                = "domain.Repository.GetPackage"
	operationGetPackageInstallation    = "domain.Repository.GetPackageInstallation"
	operationGetPackageSource          = "domain.Repository.GetPackageSource"
	operationGetPackageVersion         = "domain.Repository.GetPackageVersion"
	operationGetPricingMetadata        = "domain.Repository.GetPricingMetadata"
	operationListPackageInstallations  = "domain.Repository.ListPackageInstallations"
	operationListPackageSources        = "domain.Repository.ListPackageSources"
	operationListPackageVerifications  = "domain.Repository.ListPackageVerifications"
	operationListPackageVersions       = "domain.Repository.ListPackageVersions"
	operationListPackages              = "domain.Repository.ListPackages"
	operationUpdatePackageInstallation = "domain.Repository.UpdatePackageInstallation"
	operationUpdatePricingMetadata     = "domain.Repository.UpdatePricingMetadata"
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
	return r.runAffected(ctx, operationCreatePricingMetadata, queryPricingMetadataCreate, pricingMetadataArgs(metadata))
}

func (r *Repository) UpdatePricingMetadata(ctx context.Context, metadata entity.PackagePricingMetadata, previousVersion int64) error {
	return r.runAffected(ctx, operationUpdatePricingMetadata, queryPricingMetadataUpdate, pricingMetadataUpdateArgs(metadata, previousVersion))
}

func (r *Repository) GetPricingMetadata(ctx context.Context, packageID uuid.UUID) (entity.PackagePricingMetadata, error) {
	return queryOne(ctx, r.db, operationGetPricingMetadata, queryPricingMetadataGetByPackage, pgx.NamedArgs{"package_id": packageID}, scanPricingMetadata)
}

func (r *Repository) CreatePackageInstallation(ctx context.Context, installation entity.PackageInstallation) error {
	_, err := r.db.Exec(ctx, queryPackageInstallationCreate, packageInstallationArgs(installation))
	return wrapError(operationCreatePackageInstallation, err)
}

func (r *Repository) UpdatePackageInstallation(ctx context.Context, installation entity.PackageInstallation, previousVersion int64) error {
	return r.runAffected(ctx, operationUpdatePackageInstallation, queryPackageInstallationUpdate, packageInstallationUpdateArgs(installation, previousVersion))
}

func (r *Repository) GetPackageInstallation(ctx context.Context, id uuid.UUID) (entity.PackageInstallation, error) {
	return queryOne(ctx, r.db, operationGetPackageInstallation, queryPackageInstallationGetByID, pgx.NamedArgs{"id": id}, scanPackageInstallation)
}

func (r *Repository) ListPackageInstallations(ctx context.Context, filter query.PackageInstallationFilter) ([]entity.PackageInstallation, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackageInstallations, queryPackageInstallationList, packageInstallationFilterArgs(filter), scanPackageInstallation)
}

func (r *Repository) CreatePackageSecretSchema(ctx context.Context, schema entity.PackageSecretSchema) error {
	_, err := r.db.Exec(ctx, queryPackageSecretSchemaCreate, packageSecretSchemaArgs(schema))
	return wrapError(operationCreatePackageSecretSchema, err)
}

func (r *Repository) GetLatestPackageSecretSchema(ctx context.Context, packageVersionID uuid.UUID) (entity.PackageSecretSchema, error) {
	return queryOne(ctx, r.db, operationGetLatestSecretSchema, queryPackageSecretSchemaLatest, pgx.NamedArgs{"package_version_id": packageVersionID}, scanPackageSecretSchema)
}

func (r *Repository) CreatePackageVerification(ctx context.Context, verification entity.PackageVerification) error {
	_, err := r.db.Exec(ctx, queryPackageVerificationCreate, packageVerificationArgs(verification))
	return wrapError(operationCreatePackageVerification, err)
}

func (r *Repository) ListPackageVerifications(ctx context.Context, filter query.PackageVerificationFilter) ([]entity.PackageVerification, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackageVerifications, queryPackageVerificationList, packageVerificationFilterArgs(filter), scanPackageVerification)
}

func (r *Repository) CreateCommandResult(ctx context.Context, result entity.CommandResult) error {
	return r.runAffected(ctx, operationCreateCommandResult, queryCommandResultCreate, commandResultArgs(result))
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) runAffected(ctx context.Context, operation string, queryText string, args pgx.NamedArgs) error {
	err := postgreslib.RunMutation(ctx, r.db, errs.ErrConflict, postgreslib.Mutation{
		Query:           queryText,
		Args:            args,
		RequireAffected: true,
	})
	return wrapError(operation, err)
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
