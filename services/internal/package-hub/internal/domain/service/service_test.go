package service

import (
	"context"
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

type recordingAuthorizer struct {
	requests []AuthorizationRequest
	err      error
}

func (a *recordingAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) error {
	a.requests = append(a.requests, request)
	return a.err
}

type fakeRepository struct {
	packageEntry          entity.PackageEntry
	packageSource         entity.PackageSource
	packageVersion        entity.PackageVersion
	commandResult         entity.CommandResult
	commandResultErr      error
	getVersionCalls       int
	getCommandResultCalls int
	setVerificationCalls  int
}

func (r *fakeRepository) CreatePackageSource(context.Context, entity.PackageSource) error {
	panic("not implemented")
}

func (r *fakeRepository) GetPackageSource(context.Context, uuid.UUID) (entity.PackageSource, error) {
	return r.packageSource, nil
}

func (r *fakeRepository) ListPackageSources(context.Context, query.PackageSourceFilter) ([]entity.PackageSource, value.PageResult, error) {
	panic("not implemented")
}

func (r *fakeRepository) CreatePackage(context.Context, entity.PackageEntry) error {
	panic("not implemented")
}

func (r *fakeRepository) GetPackage(_ context.Context, _ uuid.UUID) (entity.PackageEntry, error) {
	return r.packageEntry, nil
}

func (r *fakeRepository) ListPackages(context.Context, query.PackageFilter) ([]entity.PackageEntry, value.PageResult, error) {
	panic("not implemented")
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
	panic("not implemented")
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

func (r *fakeRepository) UpdatePackageInstallation(context.Context, entity.PackageInstallation, int64) error {
	panic("not implemented")
}

func (r *fakeRepository) GetPackageInstallation(context.Context, uuid.UUID) (entity.PackageInstallation, error) {
	panic("not implemented")
}

func (r *fakeRepository) ListPackageInstallations(context.Context, query.PackageInstallationFilter) ([]entity.PackageInstallation, value.PageResult, error) {
	panic("not implemented")
}

func (r *fakeRepository) CreatePackageSecretSchema(context.Context, entity.PackageSecretSchema) error {
	panic("not implemented")
}

func (r *fakeRepository) GetLatestPackageSecretSchema(context.Context, uuid.UUID) (entity.PackageSecretSchema, error) {
	panic("not implemented")
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
