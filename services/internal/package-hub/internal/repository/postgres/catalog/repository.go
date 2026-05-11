// Package catalog implements the PostgreSQL repository for package-hub catalog data.
package catalog

import (
	"context"
	"embed"
	"time"

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

type database interface {
	execQuerier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type execQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db database
}

const (
	operationCreateManifestSnapshot          = "domain.Repository.CreateManifestSnapshot"
	operationCreatePackage                   = "domain.Repository.CreatePackage"
	operationCreatePackageInstallation       = "domain.Repository.CreatePackageInstallation"
	operationCreatePackageInstallationResult = "domain.Repository.CreatePackageInstallationWithResult"
	operationCreatePackageSecretSchema       = "domain.Repository.CreatePackageSecretSchema"
	operationCreatePackageSource             = "domain.Repository.CreatePackageSource"
	operationCreatePackageSourceResult       = "domain.Repository.CreatePackageSourceWithResult"
	operationCreatePackageVersion            = "domain.Repository.CreatePackageVersion"
	operationCreatePricingMetadata           = "domain.Repository.CreatePricingMetadata"
	operationGetCommandResult                = "domain.Repository.GetCommandResult"
	operationGetLatestManifest               = "domain.Repository.GetLatestManifestSnapshot"
	operationGetLatestSecretSchema           = "domain.Repository.GetLatestPackageSecretSchema"
	operationGetPackage                      = "domain.Repository.GetPackage"
	operationGetPackageInstallation          = "domain.Repository.GetPackageInstallation"
	operationGetPackageSource                = "domain.Repository.GetPackageSource"
	operationGetPackageVersion               = "domain.Repository.GetPackageVersion"
	operationGetPricingMetadata              = "domain.Repository.GetPricingMetadata"
	operationListPackageInstallations        = "domain.Repository.ListPackageInstallations"
	operationListPackageSources              = "domain.Repository.ListPackageSources"
	operationListPackageVerifications        = "domain.Repository.ListPackageVerifications"
	operationListPackageVersions             = "domain.Repository.ListPackageVersions"
	operationListPackages                    = "domain.Repository.ListPackages"
	operationOutboxClaim                     = "domain.Repository.ClaimOutboxEvents"
	operationOutboxMarkFailed                = "domain.Repository.MarkOutboxEventFailed"
	operationOutboxMarkPermanent             = "domain.Repository.MarkOutboxEventPermanentlyFailed"
	operationOutboxMarkPublished             = "domain.Repository.MarkOutboxEventPublished"
	operationSetPackageVerification          = "domain.Repository.SetPackageVerification"
	operationSyncAvailableCatalog            = "domain.Repository.SyncAvailableCatalog"
	operationUpdatePackageInstallation       = "domain.Repository.UpdatePackageInstallation"
	operationUpdatePackageInstallationResult = "domain.Repository.UpdatePackageInstallationWithResult"
	operationUpdatePackageSourceResult       = "domain.Repository.UpdatePackageSourceWithResult"
	operationUpdatePricingMetadata           = "domain.Repository.UpdatePricingMetadata"
)

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreatePackageSource(ctx context.Context, source entity.PackageSource) error {
	_, err := r.db.Exec(ctx, queryPackageSourceCreate, packageSourceArgs(source))
	return wrapError(operationCreatePackageSource, err)
}

func (r *Repository) CreatePackageSourceWithResult(ctx context.Context, source entity.PackageSource, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreatePackageSourceResult, queryPackageSourceCreate, packageSourceArgs(source), result, event)
}

func (r *Repository) GetPackageSource(ctx context.Context, id uuid.UUID) (entity.PackageSource, error) {
	return queryOne(ctx, r.db, operationGetPackageSource, queryPackageSourceGetByID, pgx.NamedArgs{"id": id}, scanPackageSource)
}

func (r *Repository) ListPackageSources(ctx context.Context, filter query.PackageSourceFilter) ([]entity.PackageSource, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackageSources, queryPackageSourceList, packageSourceFilterArgs(filter), scanPackageSource)
}

func (r *Repository) UpdatePackageSourceWithResult(ctx context.Context, source entity.PackageSource, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	args := packageSourceUpdateArgs(source, previousVersion)
	return r.mutateWithResult(ctx, operationUpdatePackageSourceResult, queryPackageSourceUpdate, args, result, event)
}

func (r *Repository) SyncAvailableCatalog(ctx context.Context, plan catalogrepo.CatalogSyncPlan) (catalogrepo.CatalogSyncOutcome, error) {
	var outcome catalogrepo.CatalogSyncOutcome
	if plan.BuildEvents == nil {
		return outcome, wrapError(operationSyncAvailableCatalog, errs.ErrInvalidArgument)
	}
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, affectedMutation(queryPackageSourceUpdate, packageSourceUpdateArgs(plan.Source, plan.PreviousSourceVersion))); err != nil {
			return err
		}
		outcome.Source = plan.Source
		for _, item := range plan.Items {
			syncedPackage, err := r.syncPackage(ctx, tx, item.Entry)
			if err != nil {
				return err
			}
			outcome.Packages = append(outcome.Packages, syncedPackage)
			for _, versionPlan := range item.Versions {
				versionPlan.Version.PackageID = syncedPackage.Entry.ID
				syncedVersion, err := r.syncPackageVersion(ctx, tx, versionPlan.Version)
				if err != nil {
					return err
				}
				outcome.Versions = append(outcome.Versions, syncedVersion)
				if syncedVersion.Inserted || syncedVersion.Changed {
					versionPlan.Manifest.PackageVersionID = syncedVersion.Version.ID
					if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, affectedMutation(queryManifestSnapshotCreate, manifestSnapshotArgs(versionPlan.Manifest))); err != nil {
						return err
					}
					outcome.ManifestCount++
					versionPlan.SecretSchema.PackageVersionID = syncedVersion.Version.ID
					syncedSchema, err := r.syncPackageSecretSchema(ctx, tx, versionPlan.SecretSchema, syncedPackage.Entry.ID, syncedVersion.Version.Revision)
					if err != nil {
						return err
					}
					outcome.SecretSchemas = append(outcome.SecretSchemas, syncedSchema)
					if syncedSchema.Inserted {
						outcome.SecretSchemaCount++
					}
				}
			}
		}
		if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, commandResultMutation(plan.Result)); err != nil {
			return err
		}
		events, err := plan.BuildEvents(outcome)
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := postgreslib.RunMutation(ctx, tx, errs.ErrConflict, outboxEventMutation(event)); err != nil {
				return err
			}
		}
		return nil
	})
	return outcome, wrapError(operationSyncAvailableCatalog, err)
}

func (r *Repository) syncPackage(ctx context.Context, db execQuerier, entry entity.PackageEntry) (catalogrepo.CatalogSyncPackage, error) {
	return syncCatalogRecordResult(ctx, db, queryPackageInsertIgnore, queryPackageUpdateBySourceSlug, packageArgs(entry), entry, scanPackageSyncState, packageSyncResult)
}

func (r *Repository) syncPackageVersion(ctx context.Context, db execQuerier, version entity.PackageVersion) (catalogrepo.CatalogSyncVersion, error) {
	return syncCatalogRecordResult(ctx, db, queryPackageVersionInsertIgnore, queryPackageVersionUpdateByLabel, packageVersionArgs(version), version, scanPackageVersionSyncState, packageVersionSyncResult)
}

func (r *Repository) syncPackageSecretSchema(ctx context.Context, db execQuerier, schema entity.PackageSecretSchema, packageID uuid.UUID, versionRevision int64) (catalogrepo.CatalogSyncSecretSchema, error) {
	tag, err := db.Exec(ctx, queryPackageSecretSchemaIgnore, packageSecretSchemaArgs(schema))
	if err != nil {
		return catalogrepo.CatalogSyncSecretSchema{}, err
	}
	return catalogrepo.CatalogSyncSecretSchema{
		Schema:          schema,
		PackageID:       packageID,
		VersionRevision: versionRevision,
		Inserted:        tag.RowsAffected() == 1,
	}, nil
}

type syncState[T any] struct {
	Value    T
	Inserted bool
	Changed  bool
}

func syncCatalogRecord[T any](
	ctx context.Context,
	db execQuerier,
	insertQuery string,
	updateQuery string,
	args pgx.NamedArgs,
	candidate T,
	scanUpdated func(postgreslib.RowScanner) (syncState[T], error),
) (syncState[T], error) {
	tag, err := db.Exec(ctx, insertQuery, args)
	if err != nil {
		return syncState[T]{}, err
	}
	if tag.RowsAffected() == 1 {
		return syncState[T]{Value: candidate, Inserted: true, Changed: true}, nil
	}
	return scanUpdated(db.QueryRow(ctx, updateQuery, args))
}

func syncCatalogRecordResult[T any, R any](
	ctx context.Context,
	db execQuerier,
	insertQuery string,
	updateQuery string,
	args pgx.NamedArgs,
	candidate T,
	scanUpdated func(postgreslib.RowScanner) (syncState[T], error),
	build func(syncState[T]) R,
) (R, error) {
	state, err := syncCatalogRecord(ctx, db, insertQuery, updateQuery, args, candidate, scanUpdated)
	if err != nil {
		var zero R
		return zero, err
	}
	return build(state), nil
}

func syncStateFromScan[T any](
	row postgreslib.RowScanner,
	scan func(postgreslib.RowScanner) (T, bool, bool, error),
) (syncState[T], error) {
	stored, inserted, changed, err := scan(row)
	if err != nil {
		return syncState[T]{}, err
	}
	return syncState[T]{Value: stored, Inserted: inserted, Changed: changed}, nil
}

func scanPackageSyncState(row postgreslib.RowScanner) (syncState[entity.PackageEntry], error) {
	return syncStateFromScan(row, scanPackageSync)
}

func scanPackageVersionSyncState(row postgreslib.RowScanner) (syncState[entity.PackageVersion], error) {
	return syncStateFromScan(row, scanPackageVersionSync)
}

func packageSyncResult(state syncState[entity.PackageEntry]) catalogrepo.CatalogSyncPackage {
	return catalogrepo.CatalogSyncPackage{Entry: state.Value, Inserted: state.Inserted, Changed: state.Changed}
}

func packageVersionSyncResult(state syncState[entity.PackageVersion]) catalogrepo.CatalogSyncVersion {
	return catalogrepo.CatalogSyncVersion{Version: state.Value, Inserted: state.Inserted, Changed: state.Changed}
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

func (r *Repository) CreatePackageInstallationWithResult(ctx context.Context, installation entity.PackageInstallation, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutateWithResult(ctx, operationCreatePackageInstallationResult, queryPackageInstallationCreate, packageInstallationArgs(installation), result, event)
}

func (r *Repository) UpdatePackageInstallation(ctx context.Context, installation entity.PackageInstallation, previousVersion int64) error {
	return r.runAffected(ctx, operationUpdatePackageInstallation, queryPackageInstallationUpdate, packageInstallationUpdateArgs(installation, previousVersion))
}

func (r *Repository) UpdatePackageInstallationWithResult(ctx context.Context, installation entity.PackageInstallation, previousVersion int64, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.updatePackageInstallationWithResult(ctx, packageInstallationResultUpdate{
		installation:      installation,
		previousVersion:   previousVersion,
		commandResult:     result,
		domainOutboxEvent: event,
	})
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

func (r *Repository) SetPackageVerification(ctx context.Context, version entity.PackageVersion, previousRevision int64, verification entity.PackageVerification, result entity.CommandResult, event entity.OutboxEvent) error {
	if verification.PackageVersionID != version.ID {
		return wrapError(operationSetPackageVerification, errs.ErrInvalidArgument)
	}
	return r.mutate(ctx, operationSetPackageVerification,
		affectedMutation(queryPackageVersionVerification, packageVersionVerificationArgs(version, previousRevision)),
		affectedMutation(queryPackageVerificationCreate, packageVerificationArgs(verification)),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) ListPackageVerifications(ctx context.Context, filter query.PackageVerificationFilter) ([]entity.PackageVerification, value.PageResult, error) {
	return queryPage(ctx, r.db, operationListPackageVerifications, queryPackageVerificationList, packageVerificationFilterArgs(filter), scanPackageVerification)
}

func (r *Repository) GetCommandResult(ctx context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	return queryOne(ctx, r.db, operationGetCommandResult, queryCommandResultGet, commandIdentityArgs(identity), scanCommandResult)
}

func (r *Repository) ClaimOutboxEvents(ctx context.Context, limit int, now time.Time, lockedUntil time.Time) ([]entity.OutboxEvent, error) {
	args, ok := postgreslib.OutboxClaimArgs(limit, now, lockedUntil)
	if !ok {
		return nil, wrapError(operationOutboxClaim, errs.ErrInvalidArgument)
	}
	return queryAll(ctx, r.db, operationOutboxClaim, queryOutboxEventClaim, args, scanOutboxEvent)
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID, attemptCount int, publishedAt time.Time) error {
	update := outboxPublishUpdate{id: id, attempt: attemptCount, publishedAt: publishedAt}
	ok, err := postgreslib.ExecOutboxPublished(ctx, r.db, queryOutboxEventMarkPublished, update.id, update.attempt, update.publishedAt)
	if !ok {
		return wrapError(operationOutboxMarkPublished, errs.ErrInvalidArgument)
	}
	return wrapError(operationOutboxMarkPublished, err)
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, attemptCount int, nextAttemptAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, retryOutboxUpdate(id, attemptCount, nextAttemptAt, lastError))
}

func (r *Repository) MarkOutboxEventPermanentlyFailed(ctx context.Context, id uuid.UUID, attemptCount int, failedAt time.Time, lastError string) error {
	return r.markOutboxFailure(ctx, permanentOutboxUpdate(id, attemptCount, failedAt, lastError))
}

type outboxPublishUpdate struct {
	id          uuid.UUID
	attempt     int
	publishedAt time.Time
}

type outboxFailureUpdate struct {
	operation     string
	queryText     string
	id            uuid.UUID
	attempt       int
	timestampName string
	timestamp     time.Time
	details       string
}

func retryOutboxUpdate(id uuid.UUID, attempt int, retryAt time.Time, details string) outboxFailureUpdate {
	return outboxFailureUpdate{operation: operationOutboxMarkFailed, queryText: queryOutboxEventMarkFailed, id: id, attempt: attempt, timestampName: "next_attempt_at", timestamp: retryAt, details: details}
}

func permanentOutboxUpdate(id uuid.UUID, attempt int, failedAt time.Time, details string) outboxFailureUpdate {
	return outboxFailureUpdate{operation: operationOutboxMarkPermanent, queryText: queryOutboxEventMarkPermanent, id: id, attempt: attempt, timestampName: "failed_permanently_at", timestamp: failedAt, details: details}
}

func (r *Repository) markOutboxFailure(ctx context.Context, update outboxFailureUpdate) error {
	ok, err := postgreslib.ExecOutboxDeliveryFailure(ctx, r.db, update.queryText, update.id, update.attempt, update.timestampName, update.timestamp, update.details)
	if !ok {
		return wrapError(update.operation, errs.ErrInvalidArgument)
	}
	return wrapError(update.operation, err)
}

func (r *Repository) runAffected(ctx context.Context, operation string, queryText string, args pgx.NamedArgs) error {
	err := postgreslib.RunMutation(ctx, r.db, errs.ErrConflict, affectedMutation(queryText, args))
	return wrapError(operation, err)
}

type mutation = postgreslib.Mutation

type packageInstallationResultUpdate struct {
	installation      entity.PackageInstallation
	previousVersion   int64
	commandResult     entity.CommandResult
	domainOutboxEvent entity.OutboxEvent
}

func (r *Repository) updatePackageInstallationWithResult(ctx context.Context, update packageInstallationResultUpdate) error {
	args := packageInstallationUpdateArgs(update.installation, update.previousVersion)
	return r.mutateWithResult(ctx, operationUpdatePackageInstallationResult, queryPackageInstallationUpdate, args, update.commandResult, update.domainOutboxEvent)
}

func (r *Repository) mutateWithResult(ctx context.Context, operation string, queryText string, args pgx.NamedArgs, result entity.CommandResult, event entity.OutboxEvent) error {
	return r.mutate(ctx, operation,
		affectedMutation(queryText, args),
		commandResultMutation(result),
		outboxEventMutation(event),
	)
}

func (r *Repository) mutate(ctx context.Context, operation string, mutations ...mutation) error {
	err := postgreslib.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		return postgreslib.RunDistinctMutations(ctx, tx, errs.ErrConflict, mutations...)
	})
	return wrapError(operation, err)
}

func affectedMutation(queryText string, args pgx.NamedArgs) mutation {
	return mutation{Query: queryText, Args: args, RequireAffected: true}
}

func commandResultMutation(result entity.CommandResult) mutation {
	return affectedMutation(queryCommandResultCreate, commandResultArgs(result))
}

func outboxEventMutation(event entity.OutboxEvent) mutation {
	return affectedMutation(queryOutboxEventCreate, outboxEventArgs(event))
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
	items, err := queryAll(ctx, db, operation, queryText, args.NamedArgs, scan)
	if err != nil {
		return nil, value.PageResult{}, err
	}
	return trimPage(items, args.PageSize, args.Offset), pageResult(items, args.PageSize, args.Offset), nil
}

func queryAll[T any](ctx context.Context, db execQuerier, operation string, queryText string, args pgx.NamedArgs, scan func(postgreslib.RowScanner) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, queryText, args)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	items, err := postgreslib.ScanRows(rows, scan)
	if err != nil {
		return nil, wrapError(operation, err)
	}
	return items, nil
}
