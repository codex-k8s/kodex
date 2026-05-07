package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func (s *Service) GetPackageSource(ctx context.Context, id uuid.UUID, _ value.QueryMeta) (entity.PackageSource, error) {
	return getByID(ctx, s.repository.GetPackageSource, id)
}

func (s *Service) ListPackageSources(ctx context.Context, input ListPackageSourcesInput) (ListPackageSourcesResult, error) {
	sources, page, err := s.repository.ListPackageSources(ctx, query.PackageSourceFilter{
		OrganizationID: input.OrganizationID,
		Kind:           input.Kind,
		Status:         input.Status,
		Page:           input.Page,
	})
	if err != nil {
		return ListPackageSourcesResult{}, err
	}
	return ListPackageSourcesResult{Sources: sources, Page: page}, nil
}

func (s *Service) GetPackage(ctx context.Context, id uuid.UUID, _ value.QueryMeta) (entity.PackageEntry, error) {
	return getByID(ctx, s.repository.GetPackage, id)
}

func (s *Service) ListPackages(ctx context.Context, input ListPackagesInput) (ListPackagesResult, error) {
	packages, page, err := s.repository.ListPackages(ctx, query.PackageFilter{
		SourceID:         input.SourceID,
		Kind:             input.Kind,
		Status:           input.Status,
		CommercialStatus: input.CommercialStatus,
		TrustStatus:      input.TrustStatus,
		Query:            input.Query,
		Page:             input.Page,
	})
	if err != nil {
		return ListPackagesResult{}, err
	}
	return ListPackagesResult{Packages: packages, Page: page}, nil
}

func (s *Service) GetPackageVersion(ctx context.Context, id uuid.UUID, _ value.QueryMeta) (entity.PackageVersion, error) {
	return getByID(ctx, s.repository.GetPackageVersion, id)
}

func (s *Service) ListPackageVersions(ctx context.Context, input ListPackageVersionsInput) (ListPackageVersionsResult, error) {
	if err := requireID(input.PackageID); err != nil {
		return ListPackageVersionsResult{}, err
	}
	versions, page, err := s.repository.ListPackageVersions(ctx, query.PackageVersionFilter{
		PackageID:          input.PackageID,
		VerificationStatus: input.VerificationStatus,
		ReleaseStatus:      input.ReleaseStatus,
		Page:               input.Page,
	})
	if err != nil {
		return ListPackageVersionsResult{}, err
	}
	return ListPackageVersionsResult{Versions: versions, Page: page}, nil
}

func (s *Service) GetPackageManifest(ctx context.Context, packageVersionID uuid.UUID, _ value.QueryMeta) (entity.PackageManifestSnapshot, error) {
	return getByID(ctx, s.repository.GetLatestManifestSnapshot, packageVersionID)
}

func getByID[T any](ctx context.Context, load func(context.Context, uuid.UUID) (T, error), id uuid.UUID) (T, error) {
	if err := requireID(id); err != nil {
		var zero T
		return zero, err
	}
	return load(ctx, id)
}
