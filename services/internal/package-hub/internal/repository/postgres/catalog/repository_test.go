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
	if err := repository.UpsertPricingMetadata(ctx, pricing); err != nil {
		t.Fatalf("upsert pricing: %v", err)
	}
	pricing.Kind = enum.PackagePricingKindSubscription
	pricing.Currency = "USD"
	pricing.PricePayload = []byte(`{"monthly":10}`)
	pricing.Version = 2
	pricing.UpdatedAt = now.Add(time.Hour)
	if err := repository.UpsertPricingMetadata(ctx, pricing); err != nil {
		t.Fatalf("upsert pricing update: %v", err)
	}
	storedPricing, err := repository.GetPricingMetadata(ctx, packageA.ID)
	if err != nil {
		t.Fatalf("get pricing: %v", err)
	}
	if storedPricing.Kind != enum.PackagePricingKindSubscription || storedPricing.Currency != "USD" || storedPricing.Version != 2 {
		t.Fatalf("pricing = %+v, want subscription USD v2", storedPricing)
	}
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
