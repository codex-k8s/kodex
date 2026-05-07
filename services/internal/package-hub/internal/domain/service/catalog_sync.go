package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	catalogrepo "github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/repository/catalog"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func (s *Service) SyncAvailablePackages(ctx context.Context, input SyncAvailablePackagesInput) (SyncAvailablePackagesResult, error) {
	if err := requireID(input.SourceID); err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	if err := validateCatalogSnapshot(input.Snapshot); err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	current, err := s.repository.GetPackageSource(ctx, input.SourceID)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionCatalogSync, catalogSyncResource(current)); err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	replay, ok, err := findCommandReplay(ctx, s, input.Meta, replaySpec(packageOperationCatalogSync, enum.CommandAggregateTypePackageSource, input.SourceID, catalogSyncResultFromPayload))
	if err != nil || ok {
		return replay, err
	}
	if current.Status != enum.PackageSourceStatusActive && current.Status != enum.PackageSourceStatusSyncFailed {
		return SyncAvailablePackagesResult{}, errs.ErrPreconditionFailed
	}

	now := s.clock.Now()
	source := current
	source.LastSyncAt = &now
	source.LastError = ""
	source.Status = enum.PackageSourceStatusActive
	source.Version = current.Version + 1
	source.UpdatedAt = now

	result := SyncAvailablePackagesResult{Source: source, PackageCount: int64(len(input.Snapshot.Packages)), SyncedAt: now}
	plan, err := s.catalogSyncPlan(input, result)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	result.VersionCount = countCatalogVersions(plan.Items)
	payload, err := catalogSyncPayload(result)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	plan.Result, err = commandResult(input.Meta, packageOperationCatalogSync, enum.CommandAggregateTypePackageSource, input.SourceID, payload, now)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	plan.BuildEvents = s.catalogSyncEvents(result)

	outcome, err := s.repository.SyncAvailableCatalog(ctx, plan)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	result.Source = outcome.Source
	return result, nil
}

func (s *Service) catalogSyncPlan(input SyncAvailablePackagesInput, result SyncAvailablePackagesResult) (catalogrepo.CatalogSyncPlan, error) {
	plan := catalogrepo.CatalogSyncPlan{Source: result.Source, PreviousSourceVersion: result.Source.Version - 1}
	for _, item := range input.Snapshot.Packages {
		entry := entity.PackageEntry{
			VersionedBase: entity.VersionedBase{
				ID:        s.ids.New(),
				Version:   1,
				CreatedAt: result.SyncedAt,
				UpdatedAt: result.SyncedAt,
			},
			SourceID:         &input.SourceID,
			Slug:             strings.TrimSpace(item.Slug),
			Kind:             item.Kind,
			PublisherRef:     strings.TrimSpace(item.PublisherRef),
			DisplayName:      normalizeLocalizedTexts(item.DisplayName),
			Description:      normalizeLocalizedTexts(item.Description),
			IconObjectURI:    strings.TrimSpace(item.IconObjectURI),
			CommercialStatus: item.CommercialStatus,
			TrustStatus:      item.TrustStatus,
			Status:           item.Status,
		}
		planItem := catalogrepo.CatalogSyncItem{Entry: entry}
		for _, versionItem := range item.Versions {
			version := entity.PackageVersion{
				ID:           s.ids.New(),
				PackageID:    entry.ID,
				VersionLabel: strings.TrimSpace(versionItem.VersionLabel),
				SourceRef: value.SourceRef{
					Kind:      versionItem.SourceRef.Kind,
					Ref:       strings.TrimSpace(versionItem.SourceRef.Ref),
					CommitSHA: strings.TrimSpace(versionItem.SourceRef.CommitSHA),
				},
				ManifestDigest:     strings.TrimSpace(versionItem.ManifestDigest),
				VerificationStatus: versionItem.VerificationStatus,
				ReleaseStatus:      versionItem.ReleaseStatus,
				Revision:           1,
				PublishedAt:        versionItem.PublishedAt,
				CreatedAt:          result.SyncedAt,
				UpdatedAt:          result.SyncedAt,
			}
			manifest := entity.PackageManifestSnapshot{
				ID:               s.ids.New(),
				PackageVersionID: version.ID,
				SchemaVersion:    versionItem.ManifestSchema,
				Payload:          versionItem.ManifestPayload,
				ValidationStatus: enum.PackageManifestValidationStatusValid,
				ValidationErrors: []byte(`[]`),
				CreatedAt:        result.SyncedAt,
			}
			planItem.Versions = append(planItem.Versions, catalogrepo.CatalogSyncVersionPlan{Version: version, Manifest: manifest})
		}
		plan.Items = append(plan.Items, planItem)
	}
	return plan, nil
}

func (s *Service) catalogSyncEvents(result SyncAvailablePackagesResult) catalogrepo.CatalogSyncEventBuilder {
	return func(outcome catalogrepo.CatalogSyncOutcome) ([]entity.OutboxEvent, error) {
		events := make([]entity.OutboxEvent, 0, 1+len(outcome.Packages)+len(outcome.Versions))
		synced, err := s.catalogSyncedEvent(result)
		if err != nil {
			return nil, err
		}
		events = append(events, synced)
		for _, item := range outcome.Packages {
			event, err := s.catalogPackageEvent(item, result.SyncedAt)
			if err != nil {
				return nil, err
			}
			if event.ID != uuid.Nil {
				events = append(events, event)
			}
		}
		for _, item := range outcome.Versions {
			event, err := s.catalogVersionEvent(item, result.SyncedAt)
			if err != nil {
				return nil, err
			}
			if event.ID != uuid.Nil {
				events = append(events, event)
			}
		}
		return events, nil
	}
}

func (s *Service) catalogPackageEvent(item catalogrepo.CatalogSyncPackage, occurredAt time.Time) (entity.OutboxEvent, error) {
	return catalogChangeEvent(item.Entry, item.Inserted, item.Changed, occurredAt, s.packageDiscoveredEvent, s.packageUpdatedEvent)
}

func (s *Service) catalogVersionEvent(item catalogrepo.CatalogSyncVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	return catalogChangeEvent(item.Version, item.Inserted, item.Changed, occurredAt, s.versionDiscoveredEvent, s.versionUpdatedEvent)
}

func catalogChangeEvent[T any](
	value T,
	inserted bool,
	changed bool,
	occurredAt time.Time,
	discovered func(T, time.Time) (entity.OutboxEvent, error),
	updated func(T, time.Time) (entity.OutboxEvent, error),
) (entity.OutboxEvent, error) {
	if inserted {
		return discovered(value, occurredAt)
	}
	if changed {
		return updated(value, occurredAt)
	}
	return entity.OutboxEvent{}, nil
}

func validateCatalogSnapshot(snapshot CatalogSnapshot) error {
	seenPackages := make(map[string]struct{}, len(snapshot.Packages))
	for _, item := range snapshot.Packages {
		slug := strings.TrimSpace(item.Slug)
		if _, exists := seenPackages[slug]; exists {
			return errs.ErrInvalidArgument
		}
		seenPackages[slug] = struct{}{}
		if err := validateCatalogPackage(item); err != nil {
			return err
		}
	}
	return nil
}

func validateCatalogPackage(item CatalogPackageSnapshot) error {
	if err := requireText(item.Slug); err != nil {
		return err
	}
	if err := requirePackageKind(item.Kind); err != nil {
		return err
	}
	if err := requireLocalizedTexts(item.DisplayName, true); err != nil {
		return err
	}
	if err := requireLocalizedTexts(item.Description, false); err != nil {
		return err
	}
	if err := requireCommercialStatus(item.CommercialStatus); err != nil {
		return err
	}
	if err := requireTrustStatus(item.TrustStatus); err != nil {
		return err
	}
	if err := requirePackageStatus(item.Status); err != nil {
		return err
	}
	seenVersions := make(map[string]struct{}, len(item.Versions))
	for _, version := range item.Versions {
		label := strings.TrimSpace(version.VersionLabel)
		if _, exists := seenVersions[label]; exists {
			return errs.ErrInvalidArgument
		}
		seenVersions[label] = struct{}{}
		if err := validateCatalogVersion(version); err != nil {
			return err
		}
	}
	return nil
}

func validateCatalogVersion(version CatalogVersionSnapshot) error {
	if err := requireText(version.VersionLabel); err != nil {
		return err
	}
	if err := requireSourceRefKind(version.SourceRef.Kind); err != nil {
		return err
	}
	if err := requireText(version.SourceRef.Ref); err != nil {
		return err
	}
	if err := requireText(version.ManifestDigest); err != nil {
		return err
	}
	if version.ManifestSchema <= 0 {
		return errs.ErrInvalidArgument
	}
	if err := requireManifestPayload(version.ManifestPayload); err != nil {
		return err
	}
	if err := requireVerificationStatus(version.VerificationStatus); err != nil {
		return err
	}
	return requireReleaseStatus(version.ReleaseStatus)
}

func requireLocalizedTexts(items []value.LocalizedText, requireNonEmpty bool) error {
	if requireNonEmpty && len(items) == 0 {
		return errs.ErrInvalidArgument
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		locale := strings.TrimSpace(item.Locale)
		if locale == "" || strings.TrimSpace(item.Text) == "" {
			return errs.ErrInvalidArgument
		}
		if _, exists := seen[locale]; exists {
			return errs.ErrInvalidArgument
		}
		seen[locale] = struct{}{}
	}
	return nil
}

func normalizeLocalizedTexts(items []value.LocalizedText) []value.LocalizedText {
	result := make([]value.LocalizedText, len(items))
	for index, item := range items {
		result[index] = value.LocalizedText{Locale: strings.TrimSpace(item.Locale), Text: strings.TrimSpace(item.Text)}
	}
	return result
}

func countCatalogVersions(items []catalogrepo.CatalogSyncItem) int64 {
	var count int64
	for _, item := range items {
		count += int64(len(item.Versions))
	}
	return count
}

func catalogSyncResource(source entity.PackageSource) resourceRef {
	if source.OrganizationID != nil {
		return organizationScopedResource(packageResourceCatalog, "", source.OrganizationID.String())
	}
	return globalResource(packageResourceCatalog)
}
