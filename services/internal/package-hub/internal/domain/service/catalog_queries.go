package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func (s *Service) GetPackageSource(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.PackageSource, error) {
	return readAuthorized(ctx, s, id, meta, packageActionSourceRead, s.repository.GetPackageSource, sourceResource)
}

func (s *Service) ListPackageSources(ctx context.Context, input ListPackageSourcesInput) (ListPackageSourcesResult, error) {
	if err := requireOptionalID(input.OrganizationID); err != nil {
		return ListPackageSourcesResult{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, packageActionSourceRead, listSourcesResource(input.OrganizationID)); err != nil {
		return ListPackageSourcesResult{}, err
	}
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

func (s *Service) GetPackage(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.PackageEntry, error) {
	return readAuthorized(ctx, s, id, meta, packageActionCatalogRead, s.repository.GetPackage, packageResource)
}

func (s *Service) ListPackages(ctx context.Context, input ListPackagesInput) (ListPackagesResult, error) {
	if err := requireOptionalID(input.SourceID); err != nil {
		return ListPackagesResult{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, packageActionCatalogRead, listPackagesResource(input.SourceID)); err != nil {
		return ListPackagesResult{}, err
	}
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

func (s *Service) GetPackageVersion(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.PackageVersion, error) {
	return readAuthorized(ctx, s, id, meta, packageActionCatalogRead, s.repository.GetPackageVersion, packageVersionCatalogResource)
}

func (s *Service) ListPackageVersions(ctx context.Context, input ListPackageVersionsInput) (ListPackageVersionsResult, error) {
	if err := requireID(input.PackageID); err != nil {
		return ListPackageVersionsResult{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, packageActionCatalogRead, packageScopedResource(packageResourcePackage, input.PackageID.String(), input.PackageID.String())); err != nil {
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

func (s *Service) GetPackageManifest(ctx context.Context, packageVersionID uuid.UUID, meta value.QueryMeta) (entity.PackageManifestSnapshot, error) {
	if err := requireID(packageVersionID); err != nil {
		return entity.PackageManifestSnapshot{}, err
	}
	if err := s.authorizeQuery(ctx, meta, packageActionManifestRead, versionScopedResource(packageResourceManifest, packageVersionID.String(), packageVersionID.String())); err != nil {
		return entity.PackageManifestSnapshot{}, err
	}
	return s.repository.GetLatestManifestSnapshot(ctx, packageVersionID)
}

func sourceResource(source entity.PackageSource) resourceRef {
	if source.OrganizationID != nil {
		return organizationScopedResource(packageResourceSource, source.ID.String(), source.OrganizationID.String())
	}
	return globalResourceWithID(packageResourceSource, source.ID.String())
}

func listSourcesResource(organizationID *uuid.UUID) resourceRef {
	if organizationID == nil {
		return globalResource(packageResourceSource)
	}
	return organizationScopedResource(packageResourceSource, "", organizationID.String())
}

func packageResource(entry entity.PackageEntry) resourceRef {
	if entry.SourceID != nil {
		return sourceScopedResource(packageResourcePackage, entry.ID.String(), entry.SourceID.String())
	}
	return globalResourceWithID(packageResourcePackage, entry.ID.String())
}

func listPackagesResource(sourceID *uuid.UUID) resourceRef {
	if sourceID == nil {
		return globalResource(packageResourcePackage)
	}
	return sourceScopedResource(packageResourcePackage, "", sourceID.String())
}

func packageVersionCatalogResource(version entity.PackageVersion) resourceRef {
	return packageScopedResource(packageResourcePackage, version.PackageID.String(), version.PackageID.String())
}

func readAuthorized[T any](
	ctx context.Context,
	service *Service,
	id uuid.UUID,
	meta value.QueryMeta,
	actionKey string,
	load func(context.Context, uuid.UUID) (T, error),
	resourceFor func(T) resourceRef,
) (T, error) {
	var zero T
	if err := requireID(id); err != nil {
		return zero, err
	}
	loaded, err := load(ctx, id)
	if err != nil {
		return zero, err
	}
	if err := service.authorizeQuery(ctx, meta, actionKey, resourceFor(loaded)); err != nil {
		return zero, err
	}
	return loaded, nil
}
