package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	catalogrepo "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/repository/catalog"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

var _ catalogrepo.Repository = (*fakeRepository)(nil)

func TestGetPackageAuthorizesCatalogRead(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	sourceID := uuid.New()
	organizationID := uuid.New()
	repository := &fakeRepository{packageEntry: entity.PackageEntry{
		VersionedBase: entity.VersionedBase{ID: packageID},
		SourceID:      &sourceID,
	}, packageSource: entity.PackageSource{
		VersionedBase:  entity.VersionedBase{ID: sourceID},
		OrganizationID: &organizationID,
	}}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	_, err := service.GetPackage(context.Background(), packageID, queryMeta())
	if err != nil {
		t.Fatalf("GetPackage(): %v", err)
	}
	if len(authorizer.requests) != 1 {
		t.Fatalf("authorization calls = %d, want 1", len(authorizer.requests))
	}
	request := authorizer.requests[0]
	if request.ActionKey != packageActionCatalogRead || request.ResourceType != packageResourcePackage || request.ResourceID != packageID.String() {
		t.Fatalf("authorization resource = %+v, want package catalog read on package", request)
	}
	if request.ScopeType != packageScopeOrganization || request.ScopeID != organizationID.String() {
		t.Fatalf("authorization scope = %+v, want organization %s", request, organizationID)
	}
}

func TestGetPackageSecretSchemaAuthorizesSecretRead(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	sourceID := uuid.New()
	organizationID := uuid.New()
	versionID := uuid.New()
	schemaID := uuid.New()
	repository := &fakeRepository{
		packageEntry: entity.PackageEntry{
			VersionedBase: entity.VersionedBase{ID: packageID},
			SourceID:      &sourceID,
		},
		packageSource: entity.PackageSource{
			VersionedBase:  entity.VersionedBase{ID: sourceID},
			OrganizationID: &organizationID,
		},
		packageVersion: entity.PackageVersion{ID: versionID, PackageID: packageID},
		secretSchema: entity.PackageSecretSchema{
			ID:               schemaID,
			PackageVersionID: versionID,
			SchemaDigest:     "sha256:test",
			Fields: []value.PackageSecretField{{
				Key:      "telegram_token",
				Kind:     enum.PackageSecretFieldKindToken,
				Required: true,
			}},
		},
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	schema, err := service.GetPackageSecretSchema(context.Background(), versionID, queryMeta())
	if err != nil {
		t.Fatalf("GetPackageSecretSchema(): %v", err)
	}
	if schema.ID != schemaID || len(schema.Fields) != 1 || schema.Fields[0].Key != "telegram_token" {
		t.Fatalf("schema = %+v, want stored telegram_token schema", schema)
	}
	if repository.getVersionCalls != 1 || repository.getSecretSchemaCalls != 1 {
		t.Fatalf("repository calls = getVersion:%d getSchema:%d, want version lookup and schema read", repository.getVersionCalls, repository.getSecretSchemaCalls)
	}
	if len(authorizer.requests) != 1 {
		t.Fatalf("authorization calls = %d, want 1", len(authorizer.requests))
	}
	request := authorizer.requests[0]
	if request.ActionKey != packageActionSecretRead || request.ResourceType != packageResourceSecretSchema || request.ResourceID != versionID.String() {
		t.Fatalf("authorization resource = %+v, want secret schema read on package version", request)
	}
	if request.ScopeType != packageScopeOrganization || request.ScopeID != organizationID.String() {
		t.Fatalf("authorization scope = %+v, want organization %s", request, organizationID)
	}
}

func TestSetPackageVerificationAuthorizesBeforeReplay(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	sourceID := uuid.New()
	organizationID := uuid.New()
	versionID := uuid.New()
	repository := &fakeRepository{
		packageEntry: entity.PackageEntry{
			VersionedBase: entity.VersionedBase{ID: packageID},
			SourceID:      &sourceID,
		},
		packageSource: entity.PackageSource{
			VersionedBase:  entity.VersionedBase{ID: sourceID},
			OrganizationID: &organizationID,
		},
		packageVersion: entity.PackageVersion{ID: versionID, PackageID: packageID},
	}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: &recordingAuthorizer{err: errs.ErrForbidden}})
	_, err := service.SetPackageVerification(context.Background(), SetPackageVerificationInput{
		PackageVersionID:   versionID,
		VerificationStatus: enum.PackageVerificationStatusVerified,
		Meta:               commandMeta(),
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("SetPackageVerification() err = %v, want %v", err, errs.ErrForbidden)
	}
	if repository.getVersionCalls != 1 || repository.getCommandResultCalls != 0 || repository.setVerificationCalls != 0 {
		t.Fatalf("repository calls = getVersion:%d getCommandResult:%d set:%d, want aggregate load and no replay or mutation before authorization", repository.getVersionCalls, repository.getCommandResultCalls, repository.setVerificationCalls)
	}
}

func TestSetPackageVerificationReplayUsesStoredSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	packageID := uuid.New()
	sourceID := uuid.New()
	organizationID := uuid.New()
	versionID := uuid.New()
	verification := entity.PackageVerification{
		ID:                 uuid.New(),
		PackageVersionID:   versionID,
		VerificationStatus: enum.PackageVerificationStatusRejected,
		VerifiedByActorRef: "user:owner",
		VerificationNotes:  "manual rejection",
		CreatedAt:          now,
	}
	rejectedVersion := entity.PackageVersion{
		ID:           versionID,
		PackageID:    packageID,
		VersionLabel: "v1.0.0",
		SourceRef: value.SourceRef{
			Kind:      enum.PackageVersionSourceRefKindGitTag,
			Ref:       "v1.0.0",
			CommitSHA: "abc123",
		},
		ManifestDigest:     "sha256:manifest",
		VerificationStatus: enum.PackageVerificationStatusRejected,
		ReleaseStatus:      enum.PackageReleaseStatusBlocked,
		Revision:           2,
		CreatedAt:          now.Add(-time.Hour),
		UpdatedAt:          now,
	}
	payload, err := verificationPayload(verification, rejectedVersion)
	if err != nil {
		t.Fatalf("verificationPayload(): %v", err)
	}
	meta := commandMeta()
	repository := &fakeRepository{
		packageEntry: entity.PackageEntry{
			VersionedBase: entity.VersionedBase{ID: packageID},
			SourceID:      &sourceID,
		},
		packageSource: entity.PackageSource{
			VersionedBase:  entity.VersionedBase{ID: sourceID},
			OrganizationID: &organizationID,
		},
		packageVersion: entity.PackageVersion{
			ID:                 versionID,
			PackageID:          packageID,
			VersionLabel:       "v1.0.1",
			VerificationStatus: enum.PackageVerificationStatusVerified,
			Revision:           7,
		},
		commandResult: entity.CommandResult{
			CommandID:     &meta.CommandID,
			Operation:     packageOperationVerify,
			AggregateType: enum.CommandAggregateTypePackageVersion,
			AggregateID:   versionID,
			ResultPayload: payload,
		},
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	result, err := service.SetPackageVerification(context.Background(), SetPackageVerificationInput{
		PackageVersionID:   versionID,
		VerificationStatus: enum.PackageVerificationStatusVerified,
		Meta:               meta,
	})
	if err != nil {
		t.Fatalf("SetPackageVerification(): %v", err)
	}
	if result.Version.Revision != rejectedVersion.Revision || result.Version.VerificationStatus != rejectedVersion.VerificationStatus {
		t.Fatalf("replay version = %+v, want stored rejected revision 2", result.Version)
	}
	if result.Verification.ID != verification.ID || result.Verification.VerificationNotes != verification.VerificationNotes {
		t.Fatalf("replay verification = %+v, want stored verification", result.Verification)
	}
	if repository.getVersionCalls != 1 || repository.getCommandResultCalls != 1 || repository.setVerificationCalls != 0 {
		t.Fatalf("repository calls = getVersion:%d getCommandResult:%d set:%d, want authorization aggregate load, replay lookup and no mutation", repository.getVersionCalls, repository.getCommandResultCalls, repository.setVerificationCalls)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ScopeType != packageScopeOrganization || authorizer.requests[0].ScopeID != organizationID.String() {
		t.Fatalf("authorization requests = %+v, want package.verify in organization scope", authorizer.requests)
	}
}

func TestConnectPackageSourceAuthorizesBeforeReplayAndCreatesArtifacts(t *testing.T) {
	t.Parallel()

	organizationID := uuid.New()
	repository := &fakeRepository{commandResultErr: errs.ErrNotFound}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	source, err := service.ConnectPackageSource(context.Background(), ConnectPackageSourceInput{
		OrganizationID:     &organizationID,
		Slug:               "package-store",
		DisplayName:        "Магазин пакетов",
		Kind:               enum.PackageSourceKindStorePackage,
		RepositoryRef:      " github.com/codex-k8s/kodex-package-store ",
		CatalogEndpointRef: " store.default ",
		Meta:               commandMeta(),
	})
	if err != nil {
		t.Fatalf("ConnectPackageSource(): %v", err)
	}
	if source.Status != enum.PackageSourceStatusActive || source.Version != 1 {
		t.Fatalf("source = %+v, want active v1", source)
	}
	if source.RepositoryRef != "github.com/codex-k8s/kodex-package-store" || source.CatalogEndpointRef != "store.default" {
		t.Fatalf("source refs = %q/%q, want trimmed refs", source.RepositoryRef, source.CatalogEndpointRef)
	}
	if repository.createSourceWithResultCalls != 1 || repository.getCommandResultCalls != 1 {
		t.Fatalf("repository calls = create:%d replay:%d, want create and replay lookup", repository.createSourceWithResultCalls, repository.getCommandResultCalls)
	}
	if repository.createdSource.ID != source.ID || repository.createdResult.AggregateID != source.ID || repository.createdEvent.EventType != packageEventSourceConnected {
		t.Fatalf("created artifacts = source:%+v result:%+v event:%+v, want source command artifacts", repository.createdSource, repository.createdResult, repository.createdEvent)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionSourceConnect || authorizer.requests[0].ScopeID != organizationID.String() {
		t.Fatalf("authorization requests = %+v, want source connect in organization scope", authorizer.requests)
	}
}

func TestUpdatePackageSourceReplayUsesStoredSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	sourceID := uuid.New()
	organizationID := uuid.New()
	updatedSource := entity.PackageSource{
		VersionedBase: entity.VersionedBase{
			ID:        sourceID,
			Version:   2,
			CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now,
		},
		OrganizationID:     &organizationID,
		Slug:               "package-store",
		DisplayName:        "Обновлённый магазин",
		Kind:               enum.PackageSourceKindStorePackage,
		RepositoryRef:      "github.com/codex-k8s/kodex-package-store",
		CatalogEndpointRef: "store.v2",
		Status:             enum.PackageSourceStatusBlocked,
	}
	payload, err := sourcePayload(updatedSource)
	if err != nil {
		t.Fatalf("sourcePayload(): %v", err)
	}
	meta := commandMeta()
	repository := &fakeRepository{
		packageSource: entity.PackageSource{
			VersionedBase:  entity.VersionedBase{ID: sourceID, Version: 7},
			OrganizationID: &organizationID,
			Status:         enum.PackageSourceStatusActive,
		},
		commandResult: entity.CommandResult{
			CommandID:     &meta.CommandID,
			Operation:     packageOperationSourceUpdate,
			AggregateType: enum.CommandAggregateTypePackageSource,
			AggregateID:   sourceID,
			ResultPayload: payload,
		},
	}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: &recordingAuthorizer{}})

	result, err := service.UpdatePackageSource(context.Background(), UpdatePackageSourceInput{
		SourceID: sourceID,
		Status:   ptr(enum.PackageSourceStatusDisabled),
		Meta:     meta,
	})
	if err != nil {
		t.Fatalf("UpdatePackageSource(): %v", err)
	}
	if result.DisplayName != updatedSource.DisplayName || result.Version != updatedSource.Version || result.Status != updatedSource.Status {
		t.Fatalf("replay result = %+v, want stored snapshot %+v", result, updatedSource)
	}
	if repository.updateSourceWithResultCalls != 0 {
		t.Fatalf("update calls = %d, want replay without mutation", repository.updateSourceWithResultCalls)
	}
}

func TestUpdatePackageSourceRejectsDisabledStatus(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	repository := &fakeRepository{
		packageSource: entity.PackageSource{
			VersionedBase: entity.VersionedBase{ID: sourceID, Version: 1},
			Status:        enum.PackageSourceStatusActive,
		},
		commandResultErr: errs.ErrNotFound,
	}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: &recordingAuthorizer{}})

	_, err := service.UpdatePackageSource(context.Background(), UpdatePackageSourceInput{
		SourceID: sourceID,
		Status:   ptr(enum.PackageSourceStatusDisabled),
		Meta:     commandMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("UpdatePackageSource() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.updateSourceWithResultCalls != 0 {
		t.Fatalf("update calls = %d, want no mutation for disabled status", repository.updateSourceWithResultCalls)
	}
}

func TestSyncAvailablePackagesCreatesCatalogArtifacts(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	organizationID := uuid.New()
	repository := &fakeRepository{
		packageSource: entity.PackageSource{
			VersionedBase:  entity.VersionedBase{ID: sourceID, Version: 1},
			OrganizationID: &organizationID,
			Status:         enum.PackageSourceStatusSyncFailed,
		},
		commandResultErr: errs.ErrNotFound,
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(8), Config{Authorizer: authorizer})
	manifestPayload := testCatalogManifestPayload()

	result, err := service.SyncAvailablePackages(context.Background(), SyncAvailablePackagesInput{
		SourceID: sourceID,
		Snapshot: CatalogSnapshot{Packages: []CatalogPackageSnapshot{{
			Slug:             "telegram-approver",
			Kind:             enum.PackageKindPlugin,
			PublisherRef:     "codex-k8s",
			DisplayName:      []value.LocalizedText{{Locale: "ru", Text: "Telegram-апрувер"}},
			Description:      []value.LocalizedText{{Locale: "ru", Text: "Запрашивает согласования через Telegram"}},
			CommercialStatus: enum.PackageCommercialStatusFree,
			TrustStatus:      enum.PackageTrustStatusVerified,
			Status:           enum.PackageStatusAvailable,
			Versions: []CatalogVersionSnapshot{{
				VersionLabel: "1.0.0",
				SourceRef: value.SourceRef{
					Kind: enum.PackageVersionSourceRefKindGitTag,
					Ref:  "v1.0.0",
				},
				ManifestDigest:     testManifestDigest(t, manifestPayload),
				ManifestSchema:     1,
				ManifestPayload:    manifestPayload,
				VerificationStatus: enum.PackageVerificationStatusUnverified,
				ReleaseStatus:      enum.PackageReleaseStatusActive,
			}},
		}}},
		Meta: commandMeta(),
	})
	if err != nil {
		t.Fatalf("SyncAvailablePackages(): %v", err)
	}
	if result.PackageCount != 1 || result.VersionCount != 1 || result.Source.Status != enum.PackageSourceStatusActive || result.Source.LastSyncAt == nil {
		t.Fatalf("sync result = %+v, want one package, one version and active source", result)
	}
	if repository.syncCatalogCalls != 1 || len(repository.syncPlan.Items) != 1 || len(repository.syncEvents) != 4 {
		t.Fatalf("sync calls = %d items = %d events = %d, want one call, one item, four events", repository.syncCatalogCalls, len(repository.syncPlan.Items), len(repository.syncEvents))
	}
	if repository.syncPlan.Items[0].Entry.Slug != "telegram-approver" || repository.syncPlan.Items[0].Versions[0].Manifest.ValidationStatus != enum.PackageManifestValidationStatusValid {
		t.Fatalf("sync plan = %+v, want normalized package and valid manifest", repository.syncPlan.Items[0])
	}
	schema := repository.syncPlan.Items[0].Versions[0].SecretSchema
	if schema.SchemaDigest == "" || len(schema.Fields) != 1 || schema.Fields[0].Key != "telegram_token" {
		t.Fatalf("secret schema = %+v, want telegram_token field", schema)
	}
	if repository.syncEvents[0].EventType != packageEventCatalogSynced || repository.syncEvents[1].EventType != packageEventPackageDiscovered || repository.syncEvents[2].EventType != packageEventVersionDiscovered || repository.syncEvents[3].EventType != packageEventSecretSchemaUpdated {
		t.Fatalf("sync events = %+v, want catalog, package, version and secret schema events", repository.syncEvents)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionCatalogSync || authorizer.requests[0].ResourceType != packageResourceCatalog || authorizer.requests[0].ScopeID != organizationID.String() {
		t.Fatalf("authorization requests = %+v, want catalog sync in organization scope", authorizer.requests)
	}
}

func TestSyncAvailablePackagesRejectsManifestDigestMismatch(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	repository := &fakeRepository{commandResultErr: errs.ErrNotFound}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(6), Config{Authorizer: &recordingAuthorizer{}})
	manifestPayload := testCatalogManifestPayload()

	_, err := service.SyncAvailablePackages(context.Background(), SyncAvailablePackagesInput{
		SourceID: sourceID,
		Snapshot: CatalogSnapshot{Packages: []CatalogPackageSnapshot{{
			Slug:             "telegram-approver",
			Kind:             enum.PackageKindPlugin,
			PublisherRef:     "codex-k8s",
			DisplayName:      []value.LocalizedText{{Locale: "ru", Text: "Telegram-апрувер"}},
			Description:      []value.LocalizedText{{Locale: "ru", Text: "Запрашивает согласования через Telegram"}},
			CommercialStatus: enum.PackageCommercialStatusFree,
			TrustStatus:      enum.PackageTrustStatusVerified,
			Status:           enum.PackageStatusAvailable,
			Versions: []CatalogVersionSnapshot{{
				VersionLabel: "1.0.0",
				SourceRef: value.SourceRef{
					Kind: enum.PackageVersionSourceRefKindGitTag,
					Ref:  "v1.0.0",
				},
				ManifestDigest:     "sha256:stale",
				ManifestSchema:     1,
				ManifestPayload:    manifestPayload,
				VerificationStatus: enum.PackageVerificationStatusUnverified,
				ReleaseStatus:      enum.PackageReleaseStatusActive,
			}},
		}}},
		Meta: commandMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("SyncAvailablePackages() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.syncCatalogCalls != 0 || repository.getSourceCalls != 0 {
		t.Fatalf("repository calls = sync:%d getSource:%d, want validation before reads and mutations", repository.syncCatalogCalls, repository.getSourceCalls)
	}
}

func TestSyncAvailablePackagesRejectsUnknownManifestAccessAction(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	repository := &fakeRepository{commandResultErr: errs.ErrNotFound}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(6), Config{Authorizer: &recordingAuthorizer{}})
	manifestPayload := bytes.Replace(testCatalogManifestPayload(), []byte(`"required_access_actions": ["package.installation.read"]`), []byte(`"required_access_actions": ["package.installation.reed"]`), 1)

	_, err := service.SyncAvailablePackages(context.Background(), SyncAvailablePackagesInput{
		SourceID: sourceID,
		Snapshot: CatalogSnapshot{Packages: []CatalogPackageSnapshot{{
			Slug:             "telegram-approver",
			Kind:             enum.PackageKindPlugin,
			PublisherRef:     "codex-k8s",
			DisplayName:      []value.LocalizedText{{Locale: "ru", Text: "Telegram-апрувер"}},
			Description:      []value.LocalizedText{{Locale: "ru", Text: "Запрашивает согласования через Telegram"}},
			CommercialStatus: enum.PackageCommercialStatusFree,
			TrustStatus:      enum.PackageTrustStatusVerified,
			Status:           enum.PackageStatusAvailable,
			Versions: []CatalogVersionSnapshot{{
				VersionLabel: "1.0.0",
				SourceRef: value.SourceRef{
					Kind: enum.PackageVersionSourceRefKindGitTag,
					Ref:  "v1.0.0",
				},
				ManifestDigest:     testManifestDigest(t, manifestPayload),
				ManifestSchema:     1,
				ManifestPayload:    manifestPayload,
				VerificationStatus: enum.PackageVerificationStatusUnverified,
				ReleaseStatus:      enum.PackageReleaseStatusActive,
			}},
		}}},
		Meta: commandMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("SyncAvailablePackages() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.syncCatalogCalls != 0 || repository.getSourceCalls != 0 {
		t.Fatalf("repository calls = sync:%d getSource:%d, want validation before reads and mutations", repository.syncCatalogCalls, repository.getSourceCalls)
	}
}

func TestSyncAvailablePackagesAcceptsGuidanceManifestKindPolicy(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	repository := &fakeRepository{
		packageSource: entity.PackageSource{
			VersionedBase: entity.VersionedBase{ID: sourceID, Version: 1},
			Status:        enum.PackageSourceStatusActive,
		},
		commandResultErr: errs.ErrNotFound,
	}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(8), Config{Authorizer: &recordingAuthorizer{}})
	manifestPayload := testCatalogManifestPayloadForKind(t, "go-guidelines", enum.PackageKindGuidance, []string{"guidance"}, nil, nil, nil, false)

	_, err := service.SyncAvailablePackages(context.Background(), SyncAvailablePackagesInput{
		SourceID: sourceID,
		Snapshot: CatalogSnapshot{Packages: []CatalogPackageSnapshot{{
			Slug:             "go-guidelines",
			Kind:             enum.PackageKindGuidance,
			PublisherRef:     "codex-k8s",
			DisplayName:      []value.LocalizedText{{Locale: "ru", Text: "Руководство Go"}},
			Description:      []value.LocalizedText{{Locale: "ru", Text: "Правила разработки backend на Go"}},
			CommercialStatus: enum.PackageCommercialStatusFree,
			TrustStatus:      enum.PackageTrustStatusVerified,
			Status:           enum.PackageStatusAvailable,
			Versions: []CatalogVersionSnapshot{{
				VersionLabel: "1.0.0",
				SourceRef: value.SourceRef{
					Kind: enum.PackageVersionSourceRefKindGitTag,
					Ref:  "v1.0.0",
				},
				ManifestDigest:     testManifestDigest(t, manifestPayload),
				ManifestSchema:     1,
				ManifestPayload:    manifestPayload,
				VerificationStatus: enum.PackageVerificationStatusUnverified,
				ReleaseStatus:      enum.PackageReleaseStatusActive,
			}},
		}}},
		Meta: commandMeta(),
	})
	if err != nil {
		t.Fatalf("SyncAvailablePackages(): %v", err)
	}
	if repository.syncCatalogCalls != 1 || repository.syncPlan.Items[0].Entry.Kind != enum.PackageKindGuidance {
		t.Fatalf("sync calls = %d kind = %s, want guidance package synced", repository.syncCatalogCalls, repository.syncPlan.Items[0].Entry.Kind)
	}
}

func TestSyncAvailablePackagesRejectsGuidanceManifestWithRuntime(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	repository := &fakeRepository{commandResultErr: errs.ErrNotFound}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(6), Config{Authorizer: &recordingAuthorizer{}})
	manifestPayload := testCatalogManifestPayloadForKind(t, "go-guidelines", enum.PackageKindGuidance, []string{"guidance"}, nil, nil, nil, true)

	_, err := service.SyncAvailablePackages(context.Background(), SyncAvailablePackagesInput{
		SourceID: sourceID,
		Snapshot: CatalogSnapshot{Packages: []CatalogPackageSnapshot{{
			Slug:             "go-guidelines",
			Kind:             enum.PackageKindGuidance,
			DisplayName:      []value.LocalizedText{{Locale: "ru", Text: "Руководство Go"}},
			CommercialStatus: enum.PackageCommercialStatusFree,
			TrustStatus:      enum.PackageTrustStatusVerified,
			Status:           enum.PackageStatusAvailable,
			Versions: []CatalogVersionSnapshot{{
				VersionLabel: "1.0.0",
				SourceRef: value.SourceRef{
					Kind: enum.PackageVersionSourceRefKindGitTag,
					Ref:  "v1.0.0",
				},
				ManifestDigest:     testManifestDigest(t, manifestPayload),
				ManifestSchema:     1,
				ManifestPayload:    manifestPayload,
				VerificationStatus: enum.PackageVerificationStatusUnverified,
				ReleaseStatus:      enum.PackageReleaseStatusActive,
			}},
		}}},
		Meta: commandMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("SyncAvailablePackages() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.syncCatalogCalls != 0 || repository.getSourceCalls != 0 {
		t.Fatalf("repository calls = sync:%d getSource:%d, want validation before reads and mutations", repository.syncCatalogCalls, repository.getSourceCalls)
	}
}

func TestSyncAvailablePackagesRejectsStoreManifestWithoutStoreCapability(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	repository := &fakeRepository{commandResultErr: errs.ErrNotFound}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(6), Config{Authorizer: &recordingAuthorizer{}})
	manifestPayload := testCatalogManifestPayloadForKind(t, "package-store", enum.PackageKindStore, []string{"catalog"}, []string{"package.catalog.sync"}, nil, nil, true)

	_, err := service.SyncAvailablePackages(context.Background(), SyncAvailablePackagesInput{
		SourceID: sourceID,
		Snapshot: CatalogSnapshot{Packages: []CatalogPackageSnapshot{{
			Slug:             "package-store",
			Kind:             enum.PackageKindStore,
			DisplayName:      []value.LocalizedText{{Locale: "ru", Text: "Магазин пакетов"}},
			CommercialStatus: enum.PackageCommercialStatusFree,
			TrustStatus:      enum.PackageTrustStatusVerified,
			Status:           enum.PackageStatusAvailable,
			Versions: []CatalogVersionSnapshot{{
				VersionLabel: "1.0.0",
				SourceRef: value.SourceRef{
					Kind: enum.PackageVersionSourceRefKindGitTag,
					Ref:  "v1.0.0",
				},
				ManifestDigest:     testManifestDigest(t, manifestPayload),
				ManifestSchema:     1,
				ManifestPayload:    manifestPayload,
				VerificationStatus: enum.PackageVerificationStatusUnverified,
				ReleaseStatus:      enum.PackageReleaseStatusActive,
			}},
		}}},
		Meta: commandMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("SyncAvailablePackages() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.syncCatalogCalls != 0 || repository.getSourceCalls != 0 {
		t.Fatalf("repository calls = sync:%d getSource:%d, want validation before reads and mutations", repository.syncCatalogCalls, repository.getSourceCalls)
	}
}

func TestNormalizePackageManifestRejectsForeignReservedCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		slug         string
		kind         enum.PackageKind
		capabilities []string
	}{
		{
			name:         "guidance with platform content",
			slug:         "go-guidelines",
			kind:         enum.PackageKindGuidance,
			capabilities: []string{"guidance", "platform_content"},
		},
		{
			name:         "store with guidance",
			slug:         "package-store",
			kind:         enum.PackageKindStore,
			capabilities: []string{"store", "guidance"},
		},
		{
			name:         "platform content with store",
			slug:         "platform-site",
			kind:         enum.PackageKindPlatformContent,
			capabilities: []string{"platform_content", "store"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifestPayload := testCatalogManifestPayloadForKind(t, tt.slug, tt.kind, tt.capabilities, nil, nil, nil, false)
			_, err := normalizePackageManifestPayload(
				CatalogPackageSnapshot{
					Slug:             tt.slug,
					Kind:             tt.kind,
					CommercialStatus: enum.PackageCommercialStatusFree,
					TrustStatus:      enum.PackageTrustStatusVerified,
				},
				CatalogVersionSnapshot{
					VersionLabel: "1.0.0",
					SourceRef: value.SourceRef{
						Kind: enum.PackageVersionSourceRefKindGitTag,
						Ref:  "v1.0.0",
					},
					ManifestDigest:  testManifestDigest(t, manifestPayload),
					ManifestPayload: manifestPayload,
				},
			)
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("normalizePackageManifestPayload() err = %v, want %v", err, errs.ErrInvalidArgument)
			}
		})
	}
}

func TestNormalizePackageManifestRejectsGuidanceRuntimeAndIntegrationRequirements(t *testing.T) {
	t.Parallel()

	secretFields := []value.PackageSecretField{{
		Key:      "doc_token",
		Kind:     enum.PackageSecretFieldKindToken,
		Required: true,
		DisplayName: []value.LocalizedText{{
			Locale: "ru",
			Text:   "Токен документации",
		}},
	}}
	tests := []struct {
		name            string
		platformAPIs    []string
		accessActions   []string
		secrets         []value.PackageSecretField
		runtimeRequired bool
	}{
		{
			name:            "with runtime",
			runtimeRequired: true,
		},
		{
			name:    "with secret fields",
			secrets: secretFields,
		},
		{
			name:         "with platform api",
			platformAPIs: []string{"interaction.feedback"},
		},
		{
			name:          "with access action",
			accessActions: []string{"package.installation.read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			manifestPayload := testCatalogManifestPayloadForKind(
				t,
				"go-guidelines",
				enum.PackageKindGuidance,
				[]string{"guidance"},
				tt.platformAPIs,
				tt.accessActions,
				tt.secrets,
				tt.runtimeRequired,
			)
			_, err := normalizePackageManifestPayload(
				CatalogPackageSnapshot{
					Slug:             "go-guidelines",
					Kind:             enum.PackageKindGuidance,
					CommercialStatus: enum.PackageCommercialStatusFree,
					TrustStatus:      enum.PackageTrustStatusVerified,
				},
				CatalogVersionSnapshot{
					VersionLabel: "1.0.0",
					SourceRef: value.SourceRef{
						Kind: enum.PackageVersionSourceRefKindGitTag,
						Ref:  "v1.0.0",
					},
					ManifestDigest:  testManifestDigest(t, manifestPayload),
					ManifestPayload: manifestPayload,
				},
			)
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("normalizePackageManifestPayload() err = %v, want %v", err, errs.ErrInvalidArgument)
			}
		})
	}
}

func TestListPackagesReadsGuidanceCatalog(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	kind := enum.PackageKindGuidance
	repository := &fakeRepository{
		packageEntry: entity.PackageEntry{
			VersionedBase: entity.VersionedBase{ID: packageID},
			Kind:          kind,
			Status:        enum.PackageStatusAvailable,
			TrustStatus:   enum.PackageTrustStatusVerified,
		},
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	result, err := service.ListPackages(context.Background(), ListPackagesInput{
		Kind: &kind,
		Meta: queryMeta(),
	})
	if err != nil {
		t.Fatalf("ListPackages(): %v", err)
	}
	if len(result.Packages) != 1 || result.Packages[0].ID != packageID || result.Packages[0].Kind != enum.PackageKindGuidance {
		t.Fatalf("packages = %+v, want guidance package", result.Packages)
	}
	if repository.listPackagesCalls != 1 || repository.listPackagesFilter.Kind == nil || *repository.listPackagesFilter.Kind != enum.PackageKindGuidance {
		t.Fatalf("list filter = %+v calls = %d, want guidance kind filter", repository.listPackagesFilter, repository.listPackagesCalls)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionCatalogRead {
		t.Fatalf("authorization requests = %+v, want catalog read", authorizer.requests)
	}
}

func TestListPackageInstallationsReadsGuidanceInstallations(t *testing.T) {
	t.Parallel()

	kind := enum.PackageKindGuidance
	scope := value.ScopeRef{Type: enum.PackageInstallationScopeTypeProject, Ref: uuid.NewString()}
	installation := entity.PackageInstallation{
		VersionedBase:       entity.VersionedBase{ID: uuid.New(), Version: 1},
		PackageID:           uuid.New(),
		PackageVersionID:    uuid.New(),
		Scope:               scope,
		InstallationStatus:  enum.PackageInstallationStatusActive,
		DesiredState:        enum.PackageDesiredStatePresent,
		SecretBindingStatus: enum.PackageSecretBindingStatusNotRequired,
		LastHealthStatus:    enum.PackageHealthStatusUnknown,
	}
	repository := &fakeRepository{packageInstallation: installation}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	result, err := service.ListPackageInstallations(context.Background(), ListPackageInstallationsInput{
		Scope:       &scope,
		PackageKind: &kind,
		Meta:        queryMeta(),
	})
	if err != nil {
		t.Fatalf("ListPackageInstallations(): %v", err)
	}
	if len(result.Installations) != 1 || result.Installations[0].ID != installation.ID {
		t.Fatalf("installations = %+v, want guidance installation", result.Installations)
	}
	if repository.listInstallationsCalls != 1 || repository.listInstallationsFilter.PackageKind == nil || *repository.listInstallationsFilter.PackageKind != enum.PackageKindGuidance {
		t.Fatalf("installation filter = %+v calls = %d, want guidance kind filter", repository.listInstallationsFilter, repository.listInstallationsCalls)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionInstallationRead {
		t.Fatalf("authorization requests = %+v, want installation read", authorizer.requests)
	}
}

func TestListPackagesRejectsInvalidKind(t *testing.T) {
	t.Parallel()

	invalidKind := enum.PackageKind("workflow")
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(&fakeRepository{}, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	_, err := service.ListPackages(context.Background(), ListPackagesInput{
		Kind: &invalidKind,
		Meta: queryMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListPackages() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if len(authorizer.requests) != 0 {
		t.Fatalf("authorization requests = %+v, want validation before authorization", authorizer.requests)
	}
}

func TestRequestPackageInstallationCreatesRequestedArtifacts(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	versionID := uuid.New()
	scope := value.ScopeRef{Type: enum.PackageInstallationScopeTypeProject, Ref: uuid.NewString()}
	repository := &fakeRepository{
		packageEntry: entity.PackageEntry{
			VersionedBase: entity.VersionedBase{ID: packageID},
			Status:        enum.PackageStatusAvailable,
			TrustStatus:   enum.PackageTrustStatusVerified,
		},
		packageVersion: entity.PackageVersion{
			ID:                 versionID,
			PackageID:          packageID,
			ReleaseStatus:      enum.PackageReleaseStatusActive,
			VerificationStatus: enum.PackageVerificationStatusVerified,
		},
		manifestSnapshot: entity.PackageManifestSnapshot{PackageVersionID: versionID, Payload: testCatalogManifestPayload()},
		commandResultErr: errs.ErrNotFound,
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(2), Config{Authorizer: authorizer})

	installation, err := service.RequestPackageInstallation(context.Background(), RequestPackageInstallationInput{
		PackageID:        packageID,
		PackageVersionID: versionID,
		Scope:            scope,
		Meta:             commandMeta(),
	})
	if err != nil {
		t.Fatalf("RequestPackageInstallation(): %v", err)
	}
	if installation.InstallationStatus != enum.PackageInstallationStatusRequested || installation.SecretBindingStatus != enum.PackageSecretBindingStatusMissing {
		t.Fatalf("installation = %+v, want requested with missing required secret", installation)
	}
	if installation.RuntimeRequirementDigest == "" || installation.DesiredState != enum.PackageDesiredStatePresent {
		t.Fatalf("installation requirements = %+v, want runtime digest and present desired state", installation)
	}
	if repository.createInstallationWithResultCalls != 1 || repository.createdInstallation.ID != installation.ID {
		t.Fatalf("create installation calls = %d created = %+v, want one created installation", repository.createInstallationWithResultCalls, repository.createdInstallation)
	}
	if repository.createdResult.AggregateType != enum.CommandAggregateTypeInstallation || repository.createdResult.AggregateID != installation.ID {
		t.Fatalf("command result = %+v, want installation aggregate", repository.createdResult)
	}
	if repository.createdEvent.EventType != packageEventInstallationRequested || repository.createdEvent.AggregateID != installation.ID {
		t.Fatalf("event = %+v, want installation requested event", repository.createdEvent)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionInstall || authorizer.requests[0].ScopeType != packageScopeProject || authorizer.requests[0].ScopeID != scope.Ref {
		t.Fatalf("authorization requests = %+v, want install in project scope", authorizer.requests)
	}
}

func TestPackageInstallationRequirementsDigestUsesFullRuntimeBlock(t *testing.T) {
	t.Parallel()

	runtimeWithSmallResources := bytes.Replace(
		testCatalogManifestPayload(),
		[]byte(`"workload_kind": "deployment"`),
		[]byte(`"workload_kind": "deployment", "resources": {"cpu": "250m"}`),
		1,
	)
	runtimeWithLargeResources := bytes.Replace(
		testCatalogManifestPayload(),
		[]byte(`"workload_kind": "deployment"`),
		[]byte(`"workload_kind": "deployment", "resources": {"cpu": "500m"}`),
		1,
	)

	smallRequirements, err := packageInstallationRequirementsFromManifest(runtimeWithSmallResources)
	if err != nil {
		t.Fatalf("packageInstallationRequirementsFromManifest(small): %v", err)
	}
	largeRequirements, err := packageInstallationRequirementsFromManifest(runtimeWithLargeResources)
	if err != nil {
		t.Fatalf("packageInstallationRequirementsFromManifest(large): %v", err)
	}
	if smallRequirements.RuntimeRequirementDigest == "" || largeRequirements.RuntimeRequirementDigest == "" {
		t.Fatalf("runtime digests = %q / %q, want non-empty digests", smallRequirements.RuntimeRequirementDigest, largeRequirements.RuntimeRequirementDigest)
	}
	if smallRequirements.RuntimeRequirementDigest == largeRequirements.RuntimeRequirementDigest {
		t.Fatalf("runtime digest = %q for different runtime blocks, want different digests", smallRequirements.RuntimeRequirementDigest)
	}
}

func TestRequestPackageInstallationReplayChecksRequestAndReadAccess(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	versionID := uuid.New()
	scope := value.ScopeRef{Type: enum.PackageInstallationScopeTypeRepository, Ref: uuid.NewString()}
	stored := entity.PackageInstallation{
		VersionedBase: entity.VersionedBase{
			ID:        uuid.New(),
			Version:   1,
			CreatedAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		},
		PackageID:           packageID,
		PackageVersionID:    versionID,
		Scope:               scope,
		InstallationStatus:  enum.PackageInstallationStatusActive,
		DesiredState:        enum.PackageDesiredStatePresent,
		SecretBindingStatus: enum.PackageSecretBindingStatusNotRequired,
		LastHealthStatus:    enum.PackageHealthStatusUnknown,
	}
	payload, err := installationPayload(stored)
	if err != nil {
		t.Fatalf("installationPayload(): %v", err)
	}
	meta := commandMeta()
	repository := &fakeRepository{commandResult: entity.CommandResult{
		CommandID:     &meta.CommandID,
		Operation:     packageOperationInstall,
		AggregateType: enum.CommandAggregateTypeInstallation,
		AggregateID:   stored.ID,
		ResultPayload: payload,
	}}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, fixedIDs{}, Config{Authorizer: authorizer})

	replay, err := service.RequestPackageInstallation(context.Background(), RequestPackageInstallationInput{
		PackageID:        packageID,
		PackageVersionID: versionID,
		Scope:            scope,
		Meta:             meta,
	})
	if err != nil {
		t.Fatalf("RequestPackageInstallation replay(): %v", err)
	}
	if replay.ID != stored.ID || repository.createInstallationWithResultCalls != 0 {
		t.Fatalf("replay = %+v create calls = %d, want stored installation without mutation", replay, repository.createInstallationWithResultCalls)
	}
	if len(authorizer.requests) != 2 || authorizer.requests[0].ActionKey != packageActionInstall || authorizer.requests[1].ActionKey != packageActionInstallationRead {
		t.Fatalf("authorization requests = %+v, want install check and read check on replay", authorizer.requests)
	}
}

func TestUpdatePackageInstallationChangesVersionAndWritesEvent(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	currentVersionID := uuid.New()
	nextVersionID := uuid.New()
	installationID := uuid.New()
	repository := &fakeRepository{
		packageEntry: entity.PackageEntry{
			VersionedBase: entity.VersionedBase{ID: packageID},
			Status:        enum.PackageStatusAvailable,
			TrustStatus:   enum.PackageTrustStatusVerified,
		},
		packageVersion: entity.PackageVersion{
			ID:                 nextVersionID,
			PackageID:          packageID,
			ReleaseStatus:      enum.PackageReleaseStatusActive,
			VerificationStatus: enum.PackageVerificationStatusVerified,
		},
		packageInstallation: entity.PackageInstallation{
			VersionedBase:            entity.VersionedBase{ID: installationID, Version: 1, CreatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)},
			PackageID:                packageID,
			PackageVersionID:         currentVersionID,
			Scope:                    value.ScopeRef{Type: enum.PackageInstallationScopeTypeProject, Ref: uuid.NewString()},
			InstallationStatus:       enum.PackageInstallationStatusActive,
			DesiredState:             enum.PackageDesiredStatePresent,
			SecretBindingStatus:      enum.PackageSecretBindingStatusComplete,
			LastHealthStatus:         enum.PackageHealthStatusHealthy,
			RuntimeRequirementDigest: "sha256:old",
		},
		manifestSnapshot: entity.PackageManifestSnapshot{PackageVersionID: nextVersionID, Payload: testCatalogManifestPayload()},
		commandResultErr: errs.ErrNotFound,
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(2), Config{Authorizer: authorizer})

	updated, err := service.UpdatePackageInstallation(context.Background(), UpdatePackageInstallationInput{
		InstallationID:   installationID,
		PackageVersionID: &nextVersionID,
		Meta:             commandMeta(),
	})
	if err != nil {
		t.Fatalf("UpdatePackageInstallation(): %v", err)
	}
	if updated.PackageVersionID != nextVersionID || updated.InstallationStatus != enum.PackageInstallationStatusRequested || updated.SecretBindingStatus != enum.PackageSecretBindingStatusMissing {
		t.Fatalf("updated installation = %+v, want next version with requested/missing", updated)
	}
	if updated.RuntimeRequirementDigest == "" || updated.RuntimeRequirementDigest == "sha256:old" || updated.LastHealthStatus != enum.PackageHealthStatusUnknown {
		t.Fatalf("updated requirements = %+v, want recalculated requirements and unknown health", updated)
	}
	if repository.updateInstallationWithResultCalls != 1 || repository.updatedInstallation.ID != installationID {
		t.Fatalf("update installation calls = %d updated = %+v, want one update", repository.updateInstallationWithResultCalls, repository.updatedInstallation)
	}
	if repository.updatedEvent.EventType != packageEventInstallationUpdated || repository.updatedResult.Operation != packageOperationInstallationUpdate {
		t.Fatalf("event/result = %s/%s, want installation updated result", repository.updatedEvent.EventType, repository.updatedResult.Operation)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionInstallationUpdate {
		t.Fatalf("authorization requests = %+v, want installation update", authorizer.requests)
	}
}

func TestDisablePackageInstallationWritesLifecycleEvent(t *testing.T) {
	t.Parallel()

	installationID := uuid.New()
	repository := &fakeRepository{
		packageInstallation: entity.PackageInstallation{
			VersionedBase:       entity.VersionedBase{ID: installationID, Version: 1, CreatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)},
			PackageID:           uuid.New(),
			PackageVersionID:    uuid.New(),
			Scope:               value.ScopeRef{Type: enum.PackageInstallationScopeTypeRepository, Ref: uuid.NewString()},
			InstallationStatus:  enum.PackageInstallationStatusActive,
			DesiredState:        enum.PackageDesiredStatePresent,
			SecretBindingStatus: enum.PackageSecretBindingStatusComplete,
			LastHealthStatus:    enum.PackageHealthStatusHealthy,
		},
		commandResultErr: errs.ErrNotFound,
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(1), Config{Authorizer: authorizer})

	updated, err := service.DisablePackageInstallation(context.Background(), DisablePackageInstallationInput{
		InstallationID: installationID,
		Meta:           commandMeta(),
	})
	if err != nil {
		t.Fatalf("DisablePackageInstallation(): %v", err)
	}
	if updated.InstallationStatus != enum.PackageInstallationStatusDisabled || updated.DesiredState != enum.PackageDesiredStateSuspended {
		t.Fatalf("disabled installation = %+v, want disabled/suspended", updated)
	}
	if repository.updatedEvent.EventType != packageEventInstallationDisabled || repository.updatedResult.Operation != packageOperationInstallationDisable {
		t.Fatalf("event/result = %s/%s, want installation disabled result", repository.updatedEvent.EventType, repository.updatedResult.Operation)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionInstallationDisable {
		t.Fatalf("authorization requests = %+v, want installation disable", authorizer.requests)
	}
}

func TestUninstallPackageWritesLifecycleEvent(t *testing.T) {
	t.Parallel()

	installationID := uuid.New()
	repository := &fakeRepository{
		packageInstallation: entity.PackageInstallation{
			VersionedBase:       entity.VersionedBase{ID: installationID, Version: 1, CreatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)},
			PackageID:           uuid.New(),
			PackageVersionID:    uuid.New(),
			Scope:               value.ScopeRef{Type: enum.PackageInstallationScopeTypeOrganization, Ref: uuid.NewString()},
			InstallationStatus:  enum.PackageInstallationStatusDisabled,
			DesiredState:        enum.PackageDesiredStateSuspended,
			SecretBindingStatus: enum.PackageSecretBindingStatusComplete,
			LastHealthStatus:    enum.PackageHealthStatusUnknown,
		},
		commandResultErr: errs.ErrNotFound,
	}
	authorizer := &recordingAuthorizer{}
	service := NewWithConfig(repository, fixedClock{}, newSequenceIDs(1), Config{Authorizer: authorizer})

	updated, err := service.UninstallPackage(context.Background(), UninstallPackageInput{
		InstallationID: installationID,
		Meta:           commandMeta(),
	})
	if err != nil {
		t.Fatalf("UninstallPackage(): %v", err)
	}
	if updated.InstallationStatus != enum.PackageInstallationStatusUninstalled || updated.DesiredState != enum.PackageDesiredStateAbsent {
		t.Fatalf("uninstalled installation = %+v, want uninstalled/absent", updated)
	}
	if repository.updatedEvent.EventType != packageEventInstallationUninstalled || repository.updatedResult.Operation != packageOperationUninstall {
		t.Fatalf("event/result = %s/%s, want installation uninstalled result", repository.updatedEvent.EventType, repository.updatedResult.Operation)
	}
	if len(authorizer.requests) != 1 || authorizer.requests[0].ActionKey != packageActionUninstall {
		t.Fatalf("authorization requests = %+v, want package uninstall", authorizer.requests)
	}
}

func TestUpdatePackageInstallationRejectsDedicatedLifecycleStatuses(t *testing.T) {
	t.Parallel()

	service := NewWithConfig(&fakeRepository{}, fixedClock{}, fixedIDs{}, Config{Authorizer: &recordingAuthorizer{}})
	disabled := enum.PackageInstallationStatusDisabled
	_, err := service.UpdatePackageInstallation(context.Background(), UpdatePackageInstallationInput{
		InstallationID:     uuid.New(),
		InstallationStatus: &disabled,
		Meta:               commandMeta(),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("UpdatePackageInstallation() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func testCatalogManifestPayload() []byte {
	return []byte(`{
		"identity": {
			"slug": "telegram-approver",
			"kind": "plugin",
			"publisher": "codex-k8s",
			"license": "MIT",
			"name": [{"locale": "ru", "text": "Telegram-апрувер"}],
			"description": [{"locale": "ru", "text": "Запрашивает согласования через Telegram"}]
		},
		"source": {
			"ref_kind": "git_tag",
			"ref": "v1.0.0",
			"version": "1.0.0",
			"digest": "sha256:source"
		},
		"capabilities": ["approval"],
		"required_platform_apis": ["interaction.feedback"],
		"required_access_actions": ["package.installation.read"],
		"secrets": [{
			"key": "telegram_token",
			"kind": "token",
			"required": true,
			"display_name": [{"locale": "ru", "text": "Токен Telegram"}],
			"description": [{"locale": "ru", "text": "Токен бота для запросов согласования"}]
		}],
		"runtime": {
			"required": true,
			"workload_kind": "deployment"
		},
		"pricing": {
			"commercial_status": "free"
		},
		"verification": {
			"trust_status": "verified",
			"verification_status": "unverified"
		}
	}`)
}

func testCatalogManifestPayloadForKind(
	t *testing.T,
	slug string,
	kind enum.PackageKind,
	capabilities []string,
	platformAPIs []string,
	accessActions []string,
	secrets []value.PackageSecretField,
	runtimeRequired bool,
) []byte {
	t.Helper()

	runtime := packageManifestRuntime{Required: runtimeRequired}
	if runtimeRequired {
		runtime.WorkloadKind = "deployment"
	}
	runtimePayload, err := json.Marshal(runtime)
	if err != nil {
		t.Fatalf("marshal runtime manifest block: %v", err)
	}
	document := packageManifestDocument{
		Identity: &packageManifestIdentity{
			Slug:      slug,
			Kind:      kind,
			Publisher: "codex-k8s",
			License:   "MIT",
			Name: []value.LocalizedText{{
				Locale: "ru",
				Text:   slug,
			}},
			Description: []value.LocalizedText{{
				Locale: "ru",
				Text:   "Тестовый manifest",
			}},
		},
		Source: &packageManifestSource{
			RefKind: enum.PackageVersionSourceRefKindGitTag,
			Ref:     "v1.0.0",
			Version: "1.0.0",
			Digest:  "sha256:source",
		},
		Capabilities:          capabilities,
		RequiredPlatformAPIs:  platformAPIs,
		RequiredAccessActions: accessActions,
		Secrets:               secrets,
		Runtime:               runtimePayload,
		Pricing: &packageManifestPricing{
			CommercialStatus: enum.PackageCommercialStatusFree,
		},
		Verification: &packageManifestVerification{
			TrustStatus:        enum.PackageTrustStatusVerified,
			VerificationStatus: enum.PackageVerificationStatusUnverified,
		},
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return payload
}

func testManifestDigest(t *testing.T, payload []byte) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		t.Fatalf("compact test manifest: %v", err)
	}
	sum := sha256.Sum256(compact.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:])
}

type recordingAuthorizer struct {
	requests []AuthorizationRequest
	err      error
}

func (a *recordingAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) error {
	a.requests = append(a.requests, request)
	return a.err
}

type fakeRepository struct {
	packageEntry                      entity.PackageEntry
	packageSource                     entity.PackageSource
	packageVersion                    entity.PackageVersion
	packageInstallation               entity.PackageInstallation
	manifestSnapshot                  entity.PackageManifestSnapshot
	secretSchema                      entity.PackageSecretSchema
	commandResult                     entity.CommandResult
	commandResultErr                  error
	listPackagesFilter                query.PackageFilter
	listInstallationsFilter           query.PackageInstallationFilter
	createdSource                     entity.PackageSource
	createdInstallation               entity.PackageInstallation
	createdResult                     entity.CommandResult
	createdEvent                      entity.OutboxEvent
	updatedSource                     entity.PackageSource
	updatedInstallation               entity.PackageInstallation
	updatedResult                     entity.CommandResult
	updatedEvent                      entity.OutboxEvent
	syncPlan                          catalogrepo.CatalogSyncPlan
	syncEvents                        []entity.OutboxEvent
	createSourceWithResultCalls       int
	updateSourceWithResultCalls       int
	syncCatalogCalls                  int
	listPackagesCalls                 int
	createInstallationWithResultCalls int
	updateInstallationWithResultCalls int
	getInstallationCalls              int
	listInstallationsCalls            int
	getSourceCalls                    int
	getVersionCalls                   int
	getSecretSchemaCalls              int
	getCommandResultCalls             int
	setVerificationCalls              int
}

func (r *fakeRepository) CreatePackageSource(context.Context, entity.PackageSource) error {
	panic("not implemented")
}

func (r *fakeRepository) CreatePackageSourceWithResult(_ context.Context, source entity.PackageSource, result entity.CommandResult, event entity.OutboxEvent) error {
	r.createSourceWithResultCalls++
	r.createdSource = source
	r.createdResult = result
	r.createdEvent = event
	return nil
}

func (r *fakeRepository) GetPackageSource(context.Context, uuid.UUID) (entity.PackageSource, error) {
	r.getSourceCalls++
	return r.packageSource, nil
}

func (r *fakeRepository) ListPackageSources(context.Context, query.PackageSourceFilter) ([]entity.PackageSource, value.PageResult, error) {
	panic("not implemented")
}

func (r *fakeRepository) UpdatePackageSourceWithResult(_ context.Context, source entity.PackageSource, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	r.updateSourceWithResultCalls++
	r.updatedSource = source
	r.updatedResult = result
	r.updatedEvent = event
	return nil
}

func (r *fakeRepository) SyncAvailableCatalog(_ context.Context, plan catalogrepo.CatalogSyncPlan) (catalogrepo.CatalogSyncOutcome, error) {
	r.syncCatalogCalls++
	r.syncPlan = plan
	outcome := catalogrepo.CatalogSyncOutcome{Source: plan.Source}
	for _, item := range plan.Items {
		outcome.Packages = append(outcome.Packages, catalogrepo.CatalogSyncPackage{Entry: item.Entry, Inserted: true, Changed: true})
		for _, version := range item.Versions {
			version.Version.PackageID = item.Entry.ID
			version.SecretSchema.PackageVersionID = version.Version.ID
			outcome.Versions = append(outcome.Versions, catalogrepo.CatalogSyncVersion{Version: version.Version, Inserted: true, Changed: true})
			outcome.SecretSchemas = append(outcome.SecretSchemas, catalogrepo.CatalogSyncSecretSchema{
				Schema:          version.SecretSchema,
				PackageID:       item.Entry.ID,
				VersionRevision: version.Version.Revision,
				Inserted:        true,
			})
		}
	}
	events, err := plan.BuildEvents(outcome)
	if err != nil {
		return catalogrepo.CatalogSyncOutcome{}, err
	}
	r.syncEvents = events
	return outcome, nil
}

func (r *fakeRepository) CreatePackage(context.Context, entity.PackageEntry) error {
	panic("not implemented")
}

func (r *fakeRepository) GetPackage(_ context.Context, _ uuid.UUID) (entity.PackageEntry, error) {
	return r.packageEntry, nil
}

func (r *fakeRepository) ListPackages(_ context.Context, filter query.PackageFilter) ([]entity.PackageEntry, value.PageResult, error) {
	r.listPackagesCalls++
	r.listPackagesFilter = filter
	if r.packageEntry.ID == uuid.Nil {
		return nil, value.PageResult{}, nil
	}
	return []entity.PackageEntry{r.packageEntry}, value.PageResult{}, nil
}

func (r *fakeRepository) CreatePackageVersion(context.Context, entity.PackageVersion) error {
	panic("not implemented")
}

func (r *fakeRepository) GetPackageVersion(context.Context, uuid.UUID) (entity.PackageVersion, error) {
	r.getVersionCalls++
	return r.packageVersion, nil
}

func (r *fakeRepository) ListPackageVersions(context.Context, query.PackageVersionFilter) ([]entity.PackageVersion, value.PageResult, error) {
	panic("not implemented")
}

func (r *fakeRepository) CreateManifestSnapshot(context.Context, entity.PackageManifestSnapshot) error {
	panic("not implemented")
}

func (r *fakeRepository) GetLatestManifestSnapshot(context.Context, uuid.UUID) (entity.PackageManifestSnapshot, error) {
	return r.manifestSnapshot, nil
}

func (r *fakeRepository) CreatePricingMetadata(context.Context, entity.PackagePricingMetadata) error {
	panic("not implemented")
}

func (r *fakeRepository) UpdatePricingMetadata(context.Context, entity.PackagePricingMetadata, int64) error {
	panic("not implemented")
}

func (r *fakeRepository) GetPricingMetadata(context.Context, uuid.UUID) (entity.PackagePricingMetadata, error) {
	panic("not implemented")
}

func (r *fakeRepository) CreatePackageInstallation(context.Context, entity.PackageInstallation) error {
	panic("not implemented")
}

func (r *fakeRepository) CreatePackageInstallationWithResult(_ context.Context, installation entity.PackageInstallation, result entity.CommandResult, event entity.OutboxEvent) error {
	r.createInstallationWithResultCalls++
	r.createdInstallation = installation
	r.createdResult = result
	r.createdEvent = event
	return nil
}

func (r *fakeRepository) UpdatePackageInstallation(context.Context, entity.PackageInstallation, int64) error {
	panic("not implemented")
}

func (r *fakeRepository) UpdatePackageInstallationWithResult(_ context.Context, installation entity.PackageInstallation, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	r.updateInstallationWithResultCalls++
	r.updatedInstallation = installation
	r.updatedResult = result
	r.updatedEvent = event
	return nil
}

func (r *fakeRepository) GetPackageInstallation(context.Context, uuid.UUID) (entity.PackageInstallation, error) {
	r.getInstallationCalls++
	return r.packageInstallation, nil
}

func (r *fakeRepository) ListPackageInstallations(_ context.Context, filter query.PackageInstallationFilter) ([]entity.PackageInstallation, value.PageResult, error) {
	r.listInstallationsCalls++
	r.listInstallationsFilter = filter
	if r.packageInstallation.ID == uuid.Nil {
		return nil, value.PageResult{}, nil
	}
	return []entity.PackageInstallation{r.packageInstallation}, value.PageResult{}, nil
}

func (r *fakeRepository) CreatePackageSecretSchema(context.Context, entity.PackageSecretSchema) error {
	panic("not implemented")
}

func (r *fakeRepository) GetLatestPackageSecretSchema(context.Context, uuid.UUID) (entity.PackageSecretSchema, error) {
	r.getSecretSchemaCalls++
	return r.secretSchema, nil
}

func (r *fakeRepository) SetPackageVerification(context.Context, entity.PackageVersion, int64, entity.PackageVerification, entity.CommandResult, entity.OutboxEvent) error {
	r.setVerificationCalls++
	return nil
}

func (r *fakeRepository) ListPackageVerifications(context.Context, query.PackageVerificationFilter) ([]entity.PackageVerification, value.PageResult, error) {
	panic("not implemented")
}

func (r *fakeRepository) GetCommandResult(context.Context, query.CommandIdentity) (entity.CommandResult, error) {
	r.getCommandResultCalls++
	if r.commandResultErr != nil {
		return entity.CommandResult{}, r.commandResultErr
	}
	return r.commandResult, nil
}

func (r *fakeRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	panic("not implemented")
}

func (r *fakeRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	panic("not implemented")
}

func (r *fakeRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	panic("not implemented")
}

func (r *fakeRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	panic("not implemented")
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
}

type fixedIDs struct{}

func (fixedIDs) New() uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-000000000111")
}

type sequenceIDs struct {
	items []uuid.UUID
	index int
}

func newSequenceIDs(count int) *sequenceIDs {
	items := make([]uuid.UUID, count)
	for index := range items {
		items[index] = uuid.New()
	}
	return &sequenceIDs{items: items}
}

func (ids *sequenceIDs) New() uuid.UUID {
	if ids.index >= len(ids.items) {
		return uuid.New()
	}
	id := ids.items[ids.index]
	ids.index++
	return id
}

func queryMeta() value.QueryMeta {
	return value.QueryMeta{Actor: value.Actor{Type: "user", ID: "owner"}}
}

func commandMeta() value.CommandMeta {
	revision := int64(1)
	return value.CommandMeta{
		CommandID:       uuid.New(),
		ExpectedVersion: &revision,
		Actor:           value.Actor{Type: "user", ID: "owner"},
	}
}

func ptr[T any](value T) *T {
	return &value
}
