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
	snapshot, err := normalizeCatalogSnapshot(input.Snapshot)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	input.Snapshot = snapshot
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
			secretSchema, err := packageSecretSchemaFromManifest(s.ids.New(), version.ID, manifest.Payload, result.SyncedAt)
			if err != nil {
				return catalogrepo.CatalogSyncPlan{}, err
			}
			planItem.Versions = append(planItem.Versions, catalogrepo.CatalogSyncVersionPlan{Version: version, Manifest: manifest, SecretSchema: secretSchema})
		}
		plan.Items = append(plan.Items, planItem)
	}
	return plan, nil
}

func (s *Service) catalogSyncEvents(result SyncAvailablePackagesResult) catalogrepo.CatalogSyncEventBuilder {
	return func(outcome catalogrepo.CatalogSyncOutcome) ([]entity.OutboxEvent, error) {
		events := make([]entity.OutboxEvent, 0, 1+len(outcome.Packages)+len(outcome.Versions)+len(outcome.SecretSchemas))
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
		for _, item := range outcome.SecretSchemas {
			if !item.Inserted {
				continue
			}
			event, err := s.secretSchemaUpdatedEvent(item, result.SyncedAt)
			if err != nil {
				return nil, err
			}
			events = append(events, event)
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

func normalizeCatalogSnapshot(snapshot CatalogSnapshot) (CatalogSnapshot, error) {
	result := CatalogSnapshot{ObservedAt: snapshot.ObservedAt}
	seenPackages := make(map[string]struct{}, len(snapshot.Packages))
	for _, item := range snapshot.Packages {
		normalized, err := normalizeCatalogPackage(item)
		if err != nil {
			return CatalogSnapshot{}, err
		}
		slug := normalized.Slug
		if _, exists := seenPackages[slug]; exists {
			return CatalogSnapshot{}, errs.ErrInvalidArgument
		}
		seenPackages[slug] = struct{}{}
		result.Packages = append(result.Packages, normalized)
	}
	return result, nil
}

func normalizeCatalogPackage(item CatalogPackageSnapshot) (CatalogPackageSnapshot, error) {
	normalized := item
	normalized.Slug = strings.TrimSpace(item.Slug)
	normalized.PublisherRef = strings.TrimSpace(item.PublisherRef)
	normalized.DisplayName = normalizeLocalizedTexts(item.DisplayName)
	normalized.Description = normalizeLocalizedTexts(item.Description)
	normalized.IconObjectURI = strings.TrimSpace(item.IconObjectURI)
	normalized.Versions = nil
	if err := requireText(normalized.Slug); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	if err := requirePackageKind(normalized.Kind); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	if err := requireLocalizedTexts(normalized.DisplayName, true); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	if err := requireLocalizedTexts(normalized.Description, false); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	if err := requireCommercialStatus(normalized.CommercialStatus); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	if err := requireTrustStatus(normalized.TrustStatus); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	if err := requirePackageStatus(normalized.Status); err != nil {
		return CatalogPackageSnapshot{}, err
	}
	seenVersions := make(map[string]struct{}, len(item.Versions))
	for _, version := range item.Versions {
		normalizedVersion, err := normalizeCatalogVersion(normalized, version)
		if err != nil {
			return CatalogPackageSnapshot{}, err
		}
		label := normalizedVersion.VersionLabel
		if _, exists := seenVersions[label]; exists {
			return CatalogPackageSnapshot{}, errs.ErrInvalidArgument
		}
		seenVersions[label] = struct{}{}
		normalized.Versions = append(normalized.Versions, normalizedVersion)
	}
	return normalized, nil
}

func normalizeCatalogVersion(parent CatalogPackageSnapshot, version CatalogVersionSnapshot) (CatalogVersionSnapshot, error) {
	normalized := version
	normalized.VersionLabel = strings.TrimSpace(version.VersionLabel)
	normalized.SourceRef.Ref = strings.TrimSpace(version.SourceRef.Ref)
	normalized.SourceRef.CommitSHA = strings.TrimSpace(version.SourceRef.CommitSHA)
	normalized.ManifestDigest = strings.TrimSpace(version.ManifestDigest)
	if err := requireText(normalized.VersionLabel); err != nil {
		return CatalogVersionSnapshot{}, err
	}
	if err := requireSourceRefKind(normalized.SourceRef.Kind); err != nil {
		return CatalogVersionSnapshot{}, err
	}
	if err := requireText(normalized.SourceRef.Ref); err != nil {
		return CatalogVersionSnapshot{}, err
	}
	if err := requireText(normalized.ManifestDigest); err != nil {
		return CatalogVersionSnapshot{}, err
	}
	if normalized.ManifestSchema <= 0 {
		return CatalogVersionSnapshot{}, errs.ErrInvalidArgument
	}
	payload, err := normalizePackageManifestPayload(parent, normalized)
	if err != nil {
		return CatalogVersionSnapshot{}, err
	}
	normalized.ManifestPayload = payload
	if err := requireVerificationStatus(normalized.VerificationStatus); err != nil {
		return CatalogVersionSnapshot{}, err
	}
	if err := requireReleaseStatus(normalized.ReleaseStatus); err != nil {
		return CatalogVersionSnapshot{}, err
	}
	return normalized, nil
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
