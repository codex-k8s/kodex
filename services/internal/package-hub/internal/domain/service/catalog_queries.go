package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
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
	var zero entity.PackageEntry
	if err := requireID(id); err != nil {
		return zero, err
	}
	entry, err := s.repository.GetPackage(ctx, id)
	if err != nil {
		return zero, err
	}
	resource, err := s.packageResourceForEntry(ctx, entry, packageResourcePackage, entry.ID.String())
	if err != nil {
		return zero, err
	}
	if err := s.authorizeQuery(ctx, meta, packageActionCatalogRead, resource); err != nil {
		return zero, err
	}
	return entry, nil
}

func (s *Service) ListPackages(ctx context.Context, input ListPackagesInput) (ListPackagesResult, error) {
	if err := requireOptionalID(input.SourceID); err != nil {
		return ListPackagesResult{}, err
	}
	resource, err := s.listPackagesResource(ctx, input.SourceID)
	if err != nil {
		return ListPackagesResult{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, packageActionCatalogRead, resource); err != nil {
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
	var zero entity.PackageVersion
	if err := requireID(id); err != nil {
		return zero, err
	}
	version, err := s.repository.GetPackageVersion(ctx, id)
	if err != nil {
		return zero, err
	}
	resource, err := s.packageResourceByID(ctx, version.PackageID, packageResourcePackage, version.PackageID.String())
	if err != nil {
		return zero, err
	}
	if err := s.authorizeQuery(ctx, meta, packageActionCatalogRead, resource); err != nil {
		return zero, err
	}
	return version, nil
}

func (s *Service) ListPackageVersions(ctx context.Context, input ListPackageVersionsInput) (ListPackageVersionsResult, error) {
	if err := requireID(input.PackageID); err != nil {
		return ListPackageVersionsResult{}, err
	}
	resource, err := s.packageResourceByID(ctx, input.PackageID, packageResourcePackage, input.PackageID.String())
	if err != nil {
		return ListPackageVersionsResult{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, packageActionCatalogRead, resource); err != nil {
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
	version, err := s.repository.GetPackageVersion(ctx, packageVersionID)
	if err != nil {
		return entity.PackageManifestSnapshot{}, err
	}
	resource, err := s.packageResourceByID(ctx, version.PackageID, packageResourceManifest, packageVersionID.String())
	if err != nil {
		return entity.PackageManifestSnapshot{}, err
	}
	if err := s.authorizeQuery(ctx, meta, packageActionManifestRead, resource); err != nil {
		return entity.PackageManifestSnapshot{}, err
	}
	return s.repository.GetLatestManifestSnapshot(ctx, packageVersionID)
}

func (s *Service) GetPackageInstallation(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.PackageInstallation, error) {
	return readAuthorized(ctx, s, id, meta, packageActionInstallationRead, s.repository.GetPackageInstallation, installationResource)
}

func (s *Service) ListPackageInstallations(ctx context.Context, input ListPackageInstallationsInput) (ListPackageInstallationsResult, error) {
	if err := requireOptionalInstallationScope(input.Scope); err != nil {
		return ListPackageInstallationsResult{}, err
	}
	if err := requireOptionalID(input.PackageID); err != nil {
		return ListPackageInstallationsResult{}, err
	}
	if input.PackageKind != nil {
		if err := requirePackageKind(*input.PackageKind); err != nil {
			return ListPackageInstallationsResult{}, err
		}
	}
	if input.InstallationStatus != nil {
		if err := requireInstallationStatus(*input.InstallationStatus); err != nil {
			return ListPackageInstallationsResult{}, err
		}
	}
	if input.SecretBindingStatus != nil {
		if err := requireSecretBindingStatus(*input.SecretBindingStatus); err != nil {
			return ListPackageInstallationsResult{}, err
		}
	}
	if err := s.authorizeQuery(ctx, input.Meta, packageActionInstallationRead, listInstallationsResource(input.Scope)); err != nil {
		return ListPackageInstallationsResult{}, err
	}
	installations, page, err := s.repository.ListPackageInstallations(ctx, query.PackageInstallationFilter{
		Scope:               input.Scope,
		PackageID:           input.PackageID,
		PackageKind:         input.PackageKind,
		InstallationStatus:  input.InstallationStatus,
		SecretBindingStatus: input.SecretBindingStatus,
		Page:                input.Page,
	})
	if err != nil {
		return ListPackageInstallationsResult{}, err
	}
	return ListPackageInstallationsResult{Installations: installations, Page: page}, nil
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

func packageResourceInSource(entry entity.PackageEntry, resourceType string, resourceID string, source *entity.PackageSource) resourceRef {
	if entry.SourceID != nil {
		if source != nil && source.OrganizationID != nil {
			return organizationScopedResource(resourceType, resourceID, source.OrganizationID.String())
		}
		return globalResourceWithID(resourceType, resourceID)
	}
	return globalResourceWithID(resourceType, resourceID)
}

func (s *Service) listPackagesResource(ctx context.Context, sourceID *uuid.UUID) (resourceRef, error) {
	if sourceID == nil {
		return globalResource(packageResourcePackage), nil
	}
	source, err := s.repository.GetPackageSource(ctx, *sourceID)
	if err != nil {
		return resourceRef{}, err
	}
	return packageResourceInSource(entity.PackageEntry{SourceID: sourceID}, packageResourcePackage, "", &source), nil
}

func (s *Service) packageResourceByID(ctx context.Context, packageID uuid.UUID, resourceType string, resourceID string) (resourceRef, error) {
	entry, err := s.repository.GetPackage(ctx, packageID)
	if err != nil {
		return resourceRef{}, err
	}
	return s.packageResourceForEntry(ctx, entry, resourceType, resourceID)
}

func (s *Service) packageResourceForEntry(ctx context.Context, entry entity.PackageEntry, resourceType string, resourceID string) (resourceRef, error) {
	if entry.SourceID == nil {
		return packageResourceInSource(entry, resourceType, resourceID, nil), nil
	}
	source, err := s.repository.GetPackageSource(ctx, *entry.SourceID)
	if err != nil {
		return resourceRef{}, err
	}
	return packageResourceInSource(entry, resourceType, resourceID, &source), nil
}

func (s *Service) versionVerificationResource(ctx context.Context, version entity.PackageVersion) (resourceRef, error) {
	return s.packageResourceByID(ctx, version.PackageID, packageResourceVersion, version.ID.String())
}

func installationResource(installation entity.PackageInstallation) resourceRef {
	return scopedInstallationResource(installation.ID.String(), installation.Scope)
}

func listInstallationsResource(scope *value.ScopeRef) resourceRef {
	if scope == nil {
		return globalResource(packageResourceInstallation)
	}
	return scopedInstallationResource("", *scope)
}

func scopedInstallationResource(id string, scope value.ScopeRef) resourceRef {
	switch scope.Type {
	case enum.PackageInstallationScopeTypeOrganization:
		return organizationScopedResource(packageResourceInstallation, id, scope.Ref)
	case enum.PackageInstallationScopeTypeProject:
		return resourceRef{Type: packageResourceInstallation, ID: id, ScopeType: packageScopeProject, ScopeID: scope.Ref}
	case enum.PackageInstallationScopeTypeRepository:
		return resourceRef{Type: packageResourceInstallation, ID: id, ScopeType: packageScopeRepository, ScopeID: scope.Ref}
	default:
		return globalResourceWithID(packageResourceInstallation, id)
	}
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
