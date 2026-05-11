package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	packageservice "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
	grpcruntime "google.golang.org/grpc"
)

func TestRegisterPackageHubService(t *testing.T) {
	t.Parallel()

	server := grpcruntime.NewServer()
	RegisterPackageHubService(server, fakePackageService{})
}

func TestNewServerRequiresService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer(nil) did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestGetPackageReturnsRepositoryBackedResponse(t *testing.T) {
	t.Parallel()

	packageID := uuid.New()
	response, err := NewServer(fakePackageService{packageID: packageID}).GetPackage(context.Background(), &packagesv1.GetPackageRequest{
		Meta:      &packagesv1.QueryMeta{RequestId: "test"},
		PackageId: packageID.String(),
	})
	if err != nil {
		t.Fatalf("GetPackage(): %v", err)
	}
	if response.GetPackageEntry().GetId() != packageID.String() {
		t.Fatalf("package id = %s, want %s", response.GetPackageEntry().GetId(), packageID)
	}
}

func TestConnectPackageSourceReturnsSourceResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.NewString()
	response, err := NewServer(fakePackageService{}).ConnectPackageSource(context.Background(), &packagesv1.ConnectPackageSourceRequest{
		Meta:        &packagesv1.CommandMeta{CommandId: &commandID, Actor: &packagesv1.Actor{Type: "user", Id: "owner"}},
		Slug:        "package-store",
		DisplayName: "Магазин пакетов",
		SourceKind:  packagesv1.PackageSourceKind_PACKAGE_SOURCE_KIND_STORE_PACKAGE,
	})
	if err != nil {
		t.Fatalf("ConnectPackageSource(): %v", err)
	}
	if response.GetSource().GetSlug() != "package-store" || response.GetSource().GetStatus() != packagesv1.PackageSourceStatus_PACKAGE_SOURCE_STATUS_ACTIVE {
		t.Fatalf("source response = %+v, want active package-store", response.GetSource())
	}
}

func TestRequestPackageInstallationReturnsInstallationResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.NewString()
	packageID := uuid.New()
	versionID := uuid.New()
	scopeRef := uuid.NewString()
	response, err := NewServer(fakePackageService{}).RequestPackageInstallation(context.Background(), &packagesv1.RequestPackageInstallationRequest{
		Meta:             &packagesv1.CommandMeta{CommandId: &commandID, Actor: &packagesv1.Actor{Type: "user", Id: "owner"}},
		PackageId:        packageID.String(),
		PackageVersionId: versionID.String(),
		Scope: &packagesv1.ScopeRef{
			Type: packagesv1.PackageInstallationScopeType_PACKAGE_INSTALLATION_SCOPE_TYPE_PROJECT,
			Ref:  scopeRef,
		},
	})
	if err != nil {
		t.Fatalf("RequestPackageInstallation(): %v", err)
	}
	if response.GetInstallation().GetPackageId() != packageID.String() || response.GetInstallation().GetScope().GetRef() != scopeRef {
		t.Fatalf("installation response = %+v, want package %s scope %s", response.GetInstallation(), packageID, scopeRef)
	}
}

func TestDisablePackageInstallationReturnsInstallationResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.NewString()
	installationID := uuid.New()
	response, err := NewServer(fakePackageService{}).DisablePackageInstallation(context.Background(), &packagesv1.DisablePackageInstallationRequest{
		Meta:           &packagesv1.CommandMeta{CommandId: &commandID, Actor: &packagesv1.Actor{Type: "user", Id: "owner"}},
		InstallationId: installationID.String(),
	})
	if err != nil {
		t.Fatalf("DisablePackageInstallation(): %v", err)
	}
	if response.GetInstallation().GetId() != installationID.String() || response.GetInstallation().GetInstallationStatus() != packagesv1.PackageInstallationStatus_PACKAGE_INSTALLATION_STATUS_DISABLED {
		t.Fatalf("installation response = %+v, want disabled %s", response.GetInstallation(), installationID)
	}
}

type fakePackageService struct {
	packageID uuid.UUID
}

func (fakePackageService) ConnectPackageSource(context.Context, packageservice.ConnectPackageSourceInput) (entity.PackageSource, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	return entity.PackageSource{
		VersionedBase: entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		Slug:          "package-store",
		DisplayName:   "Магазин пакетов",
		Kind:          enum.PackageSourceKindStorePackage,
		Status:        enum.PackageSourceStatusActive,
	}, nil
}

func (fakePackageService) UpdatePackageSource(context.Context, packageservice.UpdatePackageSourceInput) (entity.PackageSource, error) {
	return entity.PackageSource{}, nil
}

func (fakePackageService) DisablePackageSource(context.Context, packageservice.DisablePackageSourceInput) (entity.PackageSource, error) {
	return entity.PackageSource{}, nil
}

func (fakePackageService) GetPackageSource(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageSource, error) {
	return entity.PackageSource{}, nil
}

func (fakePackageService) ListPackageSources(context.Context, packageservice.ListPackageSourcesInput) (packageservice.ListPackageSourcesResult, error) {
	return packageservice.ListPackageSourcesResult{}, nil
}

func (fakePackageService) SyncAvailablePackages(context.Context, packageservice.SyncAvailablePackagesInput) (packageservice.SyncAvailablePackagesResult, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	return packageservice.SyncAvailablePackagesResult{
		Source: entity.PackageSource{
			VersionedBase: entity.VersionedBase{ID: uuid.New(), Version: 2, CreatedAt: now, UpdatedAt: now},
			Slug:          "package-store",
			DisplayName:   "Магазин пакетов",
			Kind:          enum.PackageSourceKindStorePackage,
			Status:        enum.PackageSourceStatusActive,
			LastSyncAt:    &now,
		},
		PackageCount: 1,
		VersionCount: 1,
		SyncedAt:     now,
	}, nil
}

func (s fakePackageService) GetPackage(_ context.Context, id uuid.UUID, _ value.QueryMeta) (entity.PackageEntry, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	if s.packageID != uuid.Nil {
		id = s.packageID
	}
	return entity.PackageEntry{
		VersionedBase: entity.VersionedBase{ID: id, Version: 1, CreatedAt: now, UpdatedAt: now},
		Slug:          "telegram-approver",
		Kind:          enum.PackageKindPlugin,
		DisplayName:   []value.LocalizedText{{Locale: "ru", Text: "Telegram-апрувер"}},
		Status:        enum.PackageStatusAvailable,
	}, nil
}

func (fakePackageService) ListPackages(context.Context, packageservice.ListPackagesInput) (packageservice.ListPackagesResult, error) {
	return packageservice.ListPackagesResult{}, nil
}

func (fakePackageService) GetPackageVersion(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageVersion, error) {
	return entity.PackageVersion{}, nil
}

func (fakePackageService) ListPackageVersions(context.Context, packageservice.ListPackageVersionsInput) (packageservice.ListPackageVersionsResult, error) {
	return packageservice.ListPackageVersionsResult{}, nil
}

func (fakePackageService) GetPackageManifest(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageManifestSnapshot, error) {
	return entity.PackageManifestSnapshot{}, nil
}

func (fakePackageService) RequestPackageInstallation(_ context.Context, input packageservice.RequestPackageInstallationInput) (entity.PackageInstallation, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	return entity.PackageInstallation{
		VersionedBase:       entity.VersionedBase{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		PackageID:           input.PackageID,
		PackageVersionID:    input.PackageVersionID,
		Scope:               input.Scope,
		InstallationStatus:  enum.PackageInstallationStatusRequested,
		DesiredState:        enum.PackageDesiredStatePresent,
		SecretBindingStatus: enum.PackageSecretBindingStatusMissing,
		LastHealthStatus:    enum.PackageHealthStatusUnknown,
	}, nil
}

func (fakePackageService) UpdatePackageInstallation(_ context.Context, input packageservice.UpdatePackageInstallationInput) (entity.PackageInstallation, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	versionID := uuid.New()
	if input.PackageVersionID != nil {
		versionID = *input.PackageVersionID
	}
	return entity.PackageInstallation{
		VersionedBase:       entity.VersionedBase{ID: input.InstallationID, Version: 2, CreatedAt: now, UpdatedAt: now},
		PackageID:           uuid.New(),
		PackageVersionID:    versionID,
		Scope:               value.ScopeRef{Type: enum.PackageInstallationScopeTypeProject, Ref: uuid.NewString()},
		InstallationStatus:  enum.PackageInstallationStatusRequested,
		DesiredState:        enum.PackageDesiredStatePresent,
		SecretBindingStatus: enum.PackageSecretBindingStatusMissing,
		LastHealthStatus:    enum.PackageHealthStatusUnknown,
	}, nil
}

func (fakePackageService) DisablePackageInstallation(_ context.Context, input packageservice.DisablePackageInstallationInput) (entity.PackageInstallation, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	return entity.PackageInstallation{
		VersionedBase:       entity.VersionedBase{ID: input.InstallationID, Version: 2, CreatedAt: now, UpdatedAt: now},
		PackageID:           uuid.New(),
		PackageVersionID:    uuid.New(),
		Scope:               value.ScopeRef{Type: enum.PackageInstallationScopeTypeProject, Ref: uuid.NewString()},
		InstallationStatus:  enum.PackageInstallationStatusDisabled,
		DesiredState:        enum.PackageDesiredStateSuspended,
		SecretBindingStatus: enum.PackageSecretBindingStatusMissing,
		LastHealthStatus:    enum.PackageHealthStatusUnknown,
	}, nil
}

func (fakePackageService) UninstallPackage(_ context.Context, input packageservice.UninstallPackageInput) (entity.PackageInstallation, error) {
	now := time.Date(2026, 5, 7, 16, 0, 0, 0, time.UTC)
	return entity.PackageInstallation{
		VersionedBase:       entity.VersionedBase{ID: input.InstallationID, Version: 2, CreatedAt: now, UpdatedAt: now},
		PackageID:           uuid.New(),
		PackageVersionID:    uuid.New(),
		Scope:               value.ScopeRef{Type: enum.PackageInstallationScopeTypeProject, Ref: uuid.NewString()},
		InstallationStatus:  enum.PackageInstallationStatusUninstalled,
		DesiredState:        enum.PackageDesiredStateAbsent,
		SecretBindingStatus: enum.PackageSecretBindingStatusMissing,
		LastHealthStatus:    enum.PackageHealthStatusUnknown,
	}, nil
}

func (fakePackageService) GetPackageInstallation(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageInstallation, error) {
	return entity.PackageInstallation{}, nil
}

func (fakePackageService) ListPackageInstallations(context.Context, packageservice.ListPackageInstallationsInput) (packageservice.ListPackageInstallationsResult, error) {
	return packageservice.ListPackageInstallationsResult{}, nil
}

func (fakePackageService) SetPackageVerification(context.Context, packageservice.SetPackageVerificationInput) (packageservice.SetPackageVerificationResult, error) {
	return packageservice.SetPackageVerificationResult{}, nil
}
