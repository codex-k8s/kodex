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

type fakePackageService struct {
	packageID uuid.UUID
}

func (fakePackageService) GetPackageSource(context.Context, uuid.UUID, value.QueryMeta) (entity.PackageSource, error) {
	return entity.PackageSource{}, nil
}

func (fakePackageService) ListPackageSources(context.Context, packageservice.ListPackageSourcesInput) (packageservice.ListPackageSourcesResult, error) {
	return packageservice.ListPackageSourcesResult{}, nil
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

func (fakePackageService) SetPackageVerification(context.Context, packageservice.SetPackageVerificationInput) (packageservice.SetPackageVerificationResult, error) {
	return packageservice.SetPackageVerificationResult{}, nil
}
