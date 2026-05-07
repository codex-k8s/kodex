package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}
	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		query, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(query) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: "23505"}, want: errs.ErrAlreadyExists},
		{name: "foreign key", err: &pgconn.PgError{Code: "23503"}, want: errs.ErrPreconditionFailed},
		{name: "check", err: &pgconn.PgError{Code: "23514"}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRepositoryIntegrationCatalogStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 7, 14, 0, 0, 0, time.UTC)
	organizationID := uuid.New()

	source := testPackageSource(organizationID, "store", now)
	if err := repository.CreatePackageSource(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}
	if err := repository.CreatePackageSource(ctx, source); !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("duplicate source err = %v, want %v", err, errs.ErrAlreadyExists)
	}
	storedSource, err := repository.GetPackageSource(ctx, source.ID)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if storedSource.Slug != source.Slug || storedSource.OrganizationID == nil || *storedSource.OrganizationID != organizationID {
		t.Fatalf("stored source = %+v, want slug %s organization %s", storedSource, source.Slug, organizationID)
	}
	sources, page, err := repository.ListPackageSources(ctx, query.PackageSourceFilter{
		OrganizationID: &organizationID,
		Status:         ptr(enum.PackageSourceStatusActive),
		Page:           value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 || page.NextPageToken != "" {
		t.Fatalf("sources = %d token %q, want one source and no next token", len(sources), page.NextPageToken)
	}

	packageA := testPackage(source.ID, "telegram-approver", enum.PackageKindPlugin, now)
	packageB := testPackage(source.ID, "go-guidelines", enum.PackageKindGuidance, now)
	if err := repository.CreatePackage(ctx, packageA); err != nil {
		t.Fatalf("create package A: %v", err)
	}
	if err := repository.CreatePackage(ctx, packageB); err != nil {
		t.Fatalf("create package B: %v", err)
	}
	storedPackage, err := repository.GetPackage(ctx, packageA.ID)
	if err != nil {
		t.Fatalf("get package: %v", err)
	}
	if storedPackage.DisplayName[0].Text != packageA.DisplayName[0].Text || storedPackage.IconObjectURI != packageA.IconObjectURI {
		t.Fatalf("stored package = %+v, want localized name and icon", storedPackage)
	}
	packages, page, err := repository.ListPackages(ctx, query.PackageFilter{
		SourceID: &source.ID,
		Query:    "telegram",
		Page:     value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list packages: %v", err)
	}
	if len(packages) != 1 || page.NextPageToken != "" || packages[0].ID != packageA.ID {
		t.Fatalf("packages = %+v token %q, want package A only", packages, page.NextPageToken)
	}
	if _, err := pool.Exec(ctx, "UPDATE package_hub_packages SET display_name = '[1]'::jsonb WHERE id = $1", packageB.ID); err != nil {
		t.Fatalf("corrupt package display name: %v", err)
	}
	if _, err := repository.GetPackage(ctx, packageB.ID); err == nil {
		t.Fatal("get package with malformed localized payload succeeded, want error")
	}

	versionA := testPackageVersion(packageA.ID, "1.0.0", now)
	versionB := testPackageVersion(packageA.ID, "1.1.0", now.Add(time.Minute))
	if err := repository.CreatePackageVersion(ctx, versionA); err != nil {
		t.Fatalf("create version A: %v", err)
	}
	if err := repository.CreatePackageVersion(ctx, versionB); err != nil {
		t.Fatalf("create version B: %v", err)
	}
	versions, page, err := repository.ListPackageVersions(ctx, query.PackageVersionFilter{
		PackageID: packageA.ID,
		Page:      value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 1 || page.NextPageToken == "" || versions[0].VersionLabel != versionB.VersionLabel {
		t.Fatalf("versions = %+v token %q, want latest page with next token", versions, page.NextPageToken)
	}
	versions, _, err = repository.ListPackageVersions(ctx, query.PackageVersionFilter{
		PackageID: packageA.ID,
		Page:      value.PageRequest{PageSize: 1, PageToken: page.NextPageToken},
	})
	if err != nil {
		t.Fatalf("list versions page 2: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionLabel != versionA.VersionLabel {
		t.Fatalf("versions page 2 = %+v, want version A", versions)
	}

	snapshot := testManifestSnapshot(versionB.ID, now.Add(2*time.Minute))
	if err := repository.CreateManifestSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("create manifest snapshot: %v", err)
	}
	latestSnapshot, err := repository.GetLatestManifestSnapshot(ctx, versionB.ID)
	if err != nil {
		t.Fatalf("get latest manifest: %v", err)
	}
	if !jsonPayloadEqual(latestSnapshot.Payload, snapshot.Payload) {
		t.Fatalf("manifest payload = %s, want %s", latestSnapshot.Payload, snapshot.Payload)
	}

	pricing := testPricing(packageA.ID, enum.PackagePricingKindFree, "", now)
	if err := repository.CreatePricingMetadata(ctx, pricing); err != nil {
		t.Fatalf("create pricing: %v", err)
	}
	if err := repository.CreatePricingMetadata(ctx, pricing); !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("duplicate pricing err = %v, want %v", err, errs.ErrAlreadyExists)
	}
	pricing.Kind = enum.PackagePricingKindSubscription
	pricing.Currency = "USD"
	pricing.PricePayload = []byte(`{"monthly":10}`)
	pricing.Version = 2
	pricing.UpdatedAt = now.Add(time.Hour)
	if err := repository.UpdatePricingMetadata(ctx, pricing, 99); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("pricing stale update err = %v, want %v", err, errs.ErrConflict)
	}
	if err := repository.UpdatePricingMetadata(ctx, pricing, 1); err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	storedPricing, err := repository.GetPricingMetadata(ctx, packageA.ID)
	if err != nil {
		t.Fatalf("get pricing: %v", err)
	}
	if storedPricing.ID != pricing.ID || storedPricing.Kind != enum.PackagePricingKindSubscription || storedPricing.Currency != "USD" || storedPricing.Version != 2 {
		t.Fatalf("pricing = %+v, want subscription USD v2", storedPricing)
	}
}

func TestRepositoryIntegrationInstallationStorage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 7, 15, 0, 0, 0, time.UTC)

	_, packageA, versionA := seedCatalog(t, ctx, repository, uuid.New(), now)
	_, _, versionB := seedCatalog(t, ctx, repository, uuid.New(), now.Add(time.Minute))

	schema := testSecretSchema(versionA.ID, now)
	if err := repository.CreatePackageSecretSchema(ctx, schema); err != nil {
		t.Fatalf("create secret schema: %v", err)
	}
	if err := repository.CreatePackageSecretSchema(ctx, schema); !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("duplicate secret schema err = %v, want %v", err, errs.ErrAlreadyExists)
	}
	storedSchema, err := repository.GetLatestPackageSecretSchema(ctx, versionA.ID)
	if err != nil {
		t.Fatalf("get latest secret schema: %v", err)
	}
	if storedSchema.SchemaDigest != schema.SchemaDigest || len(storedSchema.Fields) != 1 || storedSchema.Fields[0].Key != "telegram_token" {
		t.Fatalf("secret schema = %+v, want telegram_token field", storedSchema)
	}
	if _, err := pool.Exec(ctx, "UPDATE package_hub_package_secret_schemas SET fields = '[1]'::jsonb WHERE id = $1", schema.ID); err != nil {
		t.Fatalf("corrupt secret schema fields: %v", err)
	}
	if _, err := repository.GetLatestPackageSecretSchema(ctx, versionA.ID); err == nil {
		t.Fatal("get secret schema with malformed fields succeeded, want error")
	}

	installation := testInstallation(packageA.ID, versionA.ID, now)
	mismatchedInstallation := testInstallation(packageA.ID, versionB.ID, now)
	if err := repository.CreatePackageInstallation(ctx, mismatchedInstallation); !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("cross-package installation err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
	if err := repository.CreatePackageInstallation(ctx, installation); err != nil {
		t.Fatalf("create installation: %v", err)
	}
	if err := repository.CreatePackageInstallation(ctx, installation); !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("duplicate installation err = %v, want %v", err, errs.ErrAlreadyExists)
	}
	storedInstallation, err := repository.GetPackageInstallation(ctx, installation.ID)
	if err != nil {
		t.Fatalf("get installation: %v", err)
	}
	if storedInstallation.Scope != installation.Scope || storedInstallation.PackageVersionID != versionA.ID {
		t.Fatalf("installation = %+v, want scope %+v and version %s", storedInstallation, installation.Scope, versionA.ID)
	}
	installations, page, err := repository.ListPackageInstallations(ctx, query.PackageInstallationFilter{
		Scope:              &installation.Scope,
		PackageKind:        ptr(enum.PackageKindPlugin),
		InstallationStatus: ptr(enum.PackageInstallationStatusRequested),
		Page:               value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(installations) != 1 || page.NextPageToken != "" || installations[0].ID != installation.ID {
		t.Fatalf("installations = %+v token %q, want installation", installations, page.NextPageToken)
	}
	installation.InstallationStatus = enum.PackageInstallationStatusActive
	installation.SecretBindingStatus = enum.PackageSecretBindingStatusComplete
	installation.LastHealthStatus = enum.PackageHealthStatusHealthy
	installation.Version = 2
	installation.UpdatedAt = now.Add(time.Hour)
	if err := repository.UpdatePackageInstallation(ctx, installation, 99); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale installation update err = %v, want %v", err, errs.ErrConflict)
	}
	if err := repository.UpdatePackageInstallation(ctx, installation, 1); err != nil {
		t.Fatalf("update installation: %v", err)
	}
	storedInstallation, err = repository.GetPackageInstallation(ctx, installation.ID)
	if err != nil {
		t.Fatalf("get updated installation: %v", err)
	}
	if storedInstallation.InstallationStatus != enum.PackageInstallationStatusActive || storedInstallation.Version != 2 {
		t.Fatalf("updated installation = %+v, want active v2", storedInstallation)
	}

	verificationA := testVerification(versionA.ID, enum.PackageVerificationStatusRejected, now)
	rejectedVersion := versionA
	rejectedVersion.VerificationStatus = enum.PackageVerificationStatusRejected
	rejectedVersion.ReleaseStatus = enum.PackageReleaseStatusBlocked
	rejectedVersion.Revision = 2
	rejectedVersion.UpdatedAt = now.Add(time.Minute)
	rejectCommandID := uuid.New()
	rejectResult := testCommandResult(rejectCommandID, "package.verify", enum.CommandAggregateTypePackageVersion, versionA.ID, "", now)
	rejectEvent := testOutboxEvent(versionA.ID, "package.verification.updated", now)
	if err := repository.SetPackageVerification(ctx, rejectedVersion, 99, verificationA, rejectResult, rejectEvent); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale verification err = %v, want %v", err, errs.ErrConflict)
	}
	if err := repository.SetPackageVerification(ctx, rejectedVersion, 1, verificationA, rejectResult, rejectEvent); err != nil {
		t.Fatalf("set verification A: %v", err)
	}
	storedVersion, err := repository.GetPackageVersion(ctx, versionA.ID)
	if err != nil {
		t.Fatalf("get verified package version: %v", err)
	}
	if storedVersion.VerificationStatus != enum.PackageVerificationStatusRejected || storedVersion.ReleaseStatus != enum.PackageReleaseStatusBlocked || storedVersion.Revision != 2 {
		t.Fatalf("verified version = %+v, want rejected blocked revision 2", storedVersion)
	}
	verificationB := testVerification(versionA.ID, enum.PackageVerificationStatusVerified, now.Add(2*time.Minute))
	verifiedVersion := storedVersion
	verifiedVersion.VerificationStatus = enum.PackageVerificationStatusVerified
	verifiedVersion.ReleaseStatus = enum.PackageReleaseStatusActive
	verifiedVersion.Revision = 3
	verifiedVersion.UpdatedAt = now.Add(2 * time.Minute)
	verifyCommandID := uuid.New()
	verifyResult := testCommandResult(verifyCommandID, "package.verify", enum.CommandAggregateTypePackageVersion, versionA.ID, "verify-repeat", now.Add(2*time.Minute))
	verifyEvent := testOutboxEvent(versionA.ID, "package.verification.updated", now.Add(2*time.Minute))
	if err := repository.SetPackageVerification(ctx, verifiedVersion, 2, verificationB, verifyResult, verifyEvent); err != nil {
		t.Fatalf("set verification B: %v", err)
	}
	claimedEvents, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(3*time.Minute), now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(claimedEvents) != 2 || claimedEvents[0].EventType != "package.verification.updated" {
		t.Fatalf("claimed events = %+v, want two verification events", claimedEvents)
	}
	if claimedEvents[0].AttemptCount != 1 || claimedEvents[0].LockedUntil == nil {
		t.Fatalf("claimed event delivery = %+v, want attempt and lock", claimedEvents[0])
	}
	if err := repository.MarkOutboxEventFailed(ctx, claimedEvents[0].ID, claimedEvents[0].AttemptCount, now.Add(5*time.Minute), "temporary"); err != nil {
		t.Fatalf("mark outbox failed: %v", err)
	}
	if err := repository.MarkOutboxEventPublished(ctx, claimedEvents[1].ID, claimedEvents[1].AttemptCount, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("mark outbox published: %v", err)
	}
	reclaimedEvents, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(6*time.Minute), now.Add(7*time.Minute))
	if err != nil {
		t.Fatalf("reclaim outbox event: %v", err)
	}
	if len(reclaimedEvents) != 1 || reclaimedEvents[0].ID != claimedEvents[0].ID || reclaimedEvents[0].AttemptCount != 2 {
		t.Fatalf("reclaimed events = %+v, want retried first event", reclaimedEvents)
	}
	if err := repository.MarkOutboxEventPermanentlyFailed(ctx, reclaimedEvents[0].ID, reclaimedEvents[0].AttemptCount, now.Add(8*time.Minute), "permanent"); err != nil {
		t.Fatalf("mark outbox permanently failed: %v", err)
	}
	verifications, page, err := repository.ListPackageVerifications(ctx, query.PackageVerificationFilter{
		PackageVersionID:   versionA.ID,
		VerificationStatus: ptr(enum.PackageVerificationStatusVerified),
		Page:               value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list verifications: %v", err)
	}
	if len(verifications) != 1 || page.NextPageToken != "" || verifications[0].ID != verificationB.ID {
		t.Fatalf("verifications = %+v token %q, want verification B", verifications, page.NextPageToken)
	}

	storedCommand, err := repository.GetCommandResult(ctx, query.CommandIdentity{CommandID: &verifyCommandID})
	if err != nil {
		t.Fatalf("get command result by command id: %v", err)
	}
	if storedCommand.AggregateID != versionA.ID || string(storedCommand.ResultPayload) == "" {
		t.Fatalf("command result = %+v, want package version aggregate", storedCommand)
	}
	replayCommandID := uuid.New()
	storedCommand, err = repository.GetCommandResult(ctx, query.CommandIdentity{CommandID: &replayCommandID, Operation: verifyResult.Operation, IdempotencyKey: verifyResult.IdempotencyKey})
	if err != nil {
		t.Fatalf("get command result by idempotency key with another command id: %v", err)
	}
	if storedCommand.Key != verifyResult.Key || storedCommand.CommandID == nil || *storedCommand.CommandID != verifyCommandID {
		t.Fatalf("idempotent command = %+v, want original command id %s", storedCommand, verifyCommandID)
	}
	revokedVersion := verifiedVersion
	revokedVersion.VerificationStatus = enum.PackageVerificationStatusRevoked
	revokedVersion.ReleaseStatus = enum.PackageReleaseStatusRevoked
	revokedVersion.Revision = 4
	revokedVersion.UpdatedAt = now.Add(3 * time.Minute)
	duplicateIdempotencyResult := testCommandResult(replayCommandID, "package.verify", enum.CommandAggregateTypePackageVersion, versionA.ID, "verify-repeat", now.Add(3*time.Minute))
	duplicateEvent := testOutboxEvent(versionA.ID, "package.verification.updated", now.Add(3*time.Minute))
	if err := repository.SetPackageVerification(ctx, revokedVersion, 3, testVerification(versionA.ID, enum.PackageVerificationStatusRevoked, now.Add(3*time.Minute)), duplicateIdempotencyResult, duplicateEvent); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("duplicate idempotency verification err = %v, want %v", err, errs.ErrConflict)
	}
	storedVersion, err = repository.GetPackageVersion(ctx, versionA.ID)
	if err != nil {
		t.Fatalf("get package version after duplicate idempotency: %v", err)
	}
	if storedVersion.Revision != 3 || storedVersion.VerificationStatus != enum.PackageVerificationStatusVerified {
		t.Fatalf("version after duplicate idempotency = %+v, want verified revision 3", storedVersion)
	}
}

func seedCatalog(t *testing.T, ctx context.Context, repository *Repository, organizationID uuid.UUID, now time.Time) (entity.PackageSource, entity.PackageEntry, entity.PackageVersion) {
	t.Helper()

	source := testPackageSource(organizationID, "store-"+organizationID.String()[:8], now)
	if err := repository.CreatePackageSource(ctx, source); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	packageA := testPackage(source.ID, "telegram-approver-"+organizationID.String()[:8], enum.PackageKindPlugin, now)
	if err := repository.CreatePackage(ctx, packageA); err != nil {
		t.Fatalf("seed package: %v", err)
	}
	versionA := testPackageVersion(packageA.ID, "1.0.0", now)
	if err := repository.CreatePackageVersion(ctx, versionA); err != nil {
		t.Fatalf("seed version: %v", err)
	}
	return source, packageA, versionA
}

func testPackageSource(organizationID uuid.UUID, slug string, now time.Time) entity.PackageSource {
	lastSyncAt := now.Add(-time.Minute)
	return entity.PackageSource{
		VersionedBase:  entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		OrganizationID: &organizationID,
		Slug:           slug,
		DisplayName:    "Магазин пакетов",
		Kind:           enum.PackageSourceKindStorePackage,
		RepositoryRef:  "github.com/codex-k8s/kodex-package-store",
		Status:         enum.PackageSourceStatusActive,
		LastSyncAt:     &lastSyncAt,
	}
}

func testPackage(sourceID uuid.UUID, slug string, kind enum.PackageKind, now time.Time) entity.PackageEntry {
	displayName := "Telegram-апрувер"
	description := "Пакет для согласований через Telegram"
	if slug == "go-guidelines" {
		displayName = "Go-гайдлайны"
		description = "Пакет руководящих документов для Go-сервисов"
	}
	return entity.PackageEntry{
		VersionedBase:    entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		SourceID:         &sourceID,
		Slug:             slug,
		Kind:             kind,
		PublisherRef:     "codex-k8s",
		DisplayName:      []value.LocalizedText{{Locale: "ru", Text: displayName}},
		Description:      []value.LocalizedText{{Locale: "ru", Text: description}},
		IconObjectURI:    "s3://kodex-icons/telegram.png",
		CommercialStatus: enum.PackageCommercialStatusFree,
		TrustStatus:      enum.PackageTrustStatusVerified,
		Status:           enum.PackageStatusAvailable,
	}
}

func testPackageVersion(packageID uuid.UUID, label string, now time.Time) entity.PackageVersion {
	publishedAt := now.Add(-time.Hour)
	return entity.PackageVersion{
		ID:           uuid.New(),
		PackageID:    packageID,
		VersionLabel: label,
		SourceRef: value.SourceRef{
			Kind:      enum.PackageVersionSourceRefKindGitTag,
			Ref:       "v" + label,
			CommitSHA: strings.Repeat("a", 40),
		},
		ManifestDigest:     "sha256:" + strings.Repeat("b", 64),
		VerificationStatus: enum.PackageVerificationStatusVerified,
		ReleaseStatus:      enum.PackageReleaseStatusActive,
		Revision:           1,
		PublishedAt:        &publishedAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
}

func testManifestSnapshot(packageVersionID uuid.UUID, now time.Time) entity.PackageManifestSnapshot {
	return entity.PackageManifestSnapshot{
		ID:               uuid.New(),
		PackageVersionID: packageVersionID,
		SchemaVersion:    1,
		Payload:          []byte(`{"identity":{"slug":"telegram-approver"}}`),
		ValidationStatus: enum.PackageManifestValidationStatusValid,
		ValidationErrors: []byte(`[]`),
		CreatedAt:        now,
	}
}

func testPricing(packageID uuid.UUID, kind enum.PackagePricingKind, currency string, now time.Time) entity.PackagePricingMetadata {
	return entity.PackagePricingMetadata{
		ID:           uuid.New(),
		PackageID:    packageID,
		Kind:         kind,
		Currency:     currency,
		PricePayload: []byte(`{}`),
		Version:      1,
		UpdatedAt:    now,
	}
}

func testSecretSchema(packageVersionID uuid.UUID, now time.Time) entity.PackageSecretSchema {
	return entity.PackageSecretSchema{
		ID:               uuid.New(),
		PackageVersionID: packageVersionID,
		SchemaDigest:     "sha256:" + strings.Repeat("c", 64),
		Fields: []value.PackageSecretField{{
			Key:      "telegram_token",
			Kind:     enum.PackageSecretFieldKindToken,
			Required: true,
			DisplayName: []value.LocalizedText{{
				Locale: "ru",
				Text:   "Токен Telegram",
			}},
			Description: []value.LocalizedText{{
				Locale: "ru",
				Text:   "Токен бота для отправки запросов согласования",
			}},
		}},
		CreatedAt: now,
	}
}

func testInstallation(packageID uuid.UUID, packageVersionID uuid.UUID, now time.Time) entity.PackageInstallation {
	return entity.PackageInstallation{
		VersionedBase:    entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		PackageID:        packageID,
		PackageVersionID: packageVersionID,
		Scope: value.ScopeRef{
			Type: enum.PackageInstallationScopeTypeProject,
			Ref:  uuid.NewString(),
		},
		InstallationStatus:       enum.PackageInstallationStatusRequested,
		DesiredState:             enum.PackageDesiredStatePresent,
		RuntimeRequirementDigest: "sha256:" + strings.Repeat("d", 64),
		SecretBindingStatus:      enum.PackageSecretBindingStatusMissing,
		LastHealthStatus:         enum.PackageHealthStatusUnknown,
	}
}

func testVerification(packageVersionID uuid.UUID, status enum.PackageVerificationStatus, now time.Time) entity.PackageVerification {
	return entity.PackageVerification{
		ID:                 uuid.New(),
		PackageVersionID:   packageVersionID,
		VerificationStatus: status,
		VerifiedByActorRef: "owner:ai-da-stas",
		VerificationNotes:  "Проверка версии пакета",
		CreatedAt:          now,
	}
}

func testCommandResult(commandID uuid.UUID, operation string, aggregateType enum.CommandAggregateType, aggregateID uuid.UUID, idempotencyKey string, now time.Time) entity.CommandResult {
	return entity.CommandResult{
		Key:            operation + ":" + commandID.String(),
		CommandID:      &commandID,
		IdempotencyKey: idempotencyKey,
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		ResultPayload:  []byte(`{"aggregate_id":"` + aggregateID.String() + `"}`),
		CreatedAt:      now,
	}
}

func testOutboxEvent(packageVersionID uuid.UUID, eventType string, now time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{Event: outboxlib.Event{
		ID:            uuid.New(),
		AggregateType: "package_version",
		AggregateID:   packageVersionID,
		EventType:     eventType,
		SchemaVersion: 1,
		Payload:       []byte(`{"package_version_id":"` + packageVersionID.String() + `"}`),
		OccurredAt:    now,
	}}
}

func ptr[T any](value T) *T {
	return &value
}

func jsonPayloadEqual(left []byte, right []byte) bool {
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("KODEX_PACKAGE_HUB_TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set KODEX_PACKAGE_HUB_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "package_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	applyMigrations(t, ctx, pool)
	return pool
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	files, err := filepath.Glob("../../../../cmd/cli/migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		for _, statement := range splitSQLStatements(upMigrationSQL(t, string(content), file)) {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("apply migration %s statement %q: %v", file, statement, err)
			}
		}
	}
}

func upMigrationSQL(t *testing.T, content string, file string) string {
	t.Helper()

	upIndex := strings.Index(content, "-- +goose Up")
	downIndex := strings.Index(content, "-- +goose Down")
	if upIndex < 0 || downIndex < 0 || downIndex < upIndex {
		t.Fatalf("invalid goose migration markers in %s", file)
	}
	return content[upIndex+len("-- +goose Up") : downIndex]
}

func splitSQLStatements(content string) []string {
	parts := strings.Split(content, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
