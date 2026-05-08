package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func (s *Service) event(eventType string, aggregateType string, aggregateID uuid.UUID, payload value.PackageEventPayload, occurredAt time.Time) (entity.OutboxEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, fmt.Errorf("marshal package event payload %s: %w", eventType, err)
	}
	return entity.OutboxEvent{Event: outboxlib.Event{
		ID:            s.ids.New(),
		EventType:     eventType,
		SchemaVersion: packageevents.SchemaVersion,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       raw,
		OccurredAt:    occurredAt,
	}}, nil
}

func (s *Service) verificationUpdatedEvent(version entity.PackageVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.versionStateEvent(packageEventVerificationUpdated, version, occurredAt)
}

func (s *Service) installationRequestedEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.installationStateEvent(packageEventInstallationRequested, installation, occurredAt)
}

func (s *Service) installationActivatedEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.installationStateEvent(packageEventInstallationActivated, installation, occurredAt)
}

func (s *Service) installationUpdatedEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.installationStateEvent(packageEventInstallationUpdated, installation, occurredAt)
}

func (s *Service) installationDisabledEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.installationStateEvent(packageEventInstallationDisabled, installation, occurredAt)
}

func (s *Service) installationUninstalledEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.installationStateEvent(packageEventInstallationUninstalled, installation, occurredAt)
}

func (s *Service) installationStateEvent(eventType string, installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(eventType, packageAggregateInstallation, installation.ID, value.PackageEventPayload{
		InstallationID:      installation.ID.String(),
		PackageID:           installation.PackageID.String(),
		PackageVersionID:    installation.PackageVersionID.String(),
		ScopeType:           string(installation.Scope.Type),
		ScopeRef:            installation.Scope.Ref,
		InstallationStatus:  string(installation.InstallationStatus),
		DesiredState:        string(installation.DesiredState),
		SecretBindingStatus: string(installation.SecretBindingStatus),
		Version:             installation.Version,
	}, occurredAt)
}

func (s *Service) catalogSyncedEvent(result SyncAvailablePackagesResult) (entity.OutboxEvent, error) {
	return s.event(packageEventCatalogSynced, packageAggregateSource, result.Source.ID, value.PackageEventPayload{
		SourceID:     result.Source.ID.String(),
		SyncedAt:     result.SyncedAt.Format(time.RFC3339Nano),
		PackageCount: result.PackageCount,
		VersionCount: result.VersionCount,
	}, result.SyncedAt)
}

func (s *Service) packageDiscoveredEvent(entry entity.PackageEntry, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(packageEventPackageDiscovered, packageAggregatePackage, entry.ID, value.PackageEventPayload{
		PackageID:   entry.ID.String(),
		SourceID:    formatOptionalUUID(entry.SourceID),
		Slug:        entry.Slug,
		PackageKind: string(entry.Kind),
	}, occurredAt)
}

func (s *Service) packageUpdatedEvent(entry entity.PackageEntry, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(packageEventPackageUpdated, packageAggregatePackage, entry.ID, value.PackageEventPayload{
		PackageID:   entry.ID.String(),
		Slug:        entry.Slug,
		Status:      string(entry.Status),
		TrustStatus: string(entry.TrustStatus),
		Version:     entry.Version,
	}, occurredAt)
}

func (s *Service) versionDiscoveredEvent(version entity.PackageVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(packageEventVersionDiscovered, packageAggregateVersion, version.ID, value.PackageEventPayload{
		PackageID:        version.PackageID.String(),
		PackageVersionID: version.ID.String(),
		VersionLabel:     version.VersionLabel,
		ManifestDigest:   version.ManifestDigest,
	}, occurredAt)
}

func (s *Service) versionUpdatedEvent(version entity.PackageVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.versionStateEvent(packageEventVersionUpdated, version, occurredAt)
}

func (s *Service) versionStateEvent(eventType string, version entity.PackageVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(eventType, packageAggregateVersion, version.ID, value.PackageEventPayload{
		PackageID:          version.PackageID.String(),
		PackageVersionID:   version.ID.String(),
		VerificationStatus: string(version.VerificationStatus),
		ReleaseStatus:      string(version.ReleaseStatus),
		Revision:           version.Revision,
	}, occurredAt)
}

func (s *Service) sourceConnectedEvent(source entity.PackageSource, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.sourceEvent(packageEventSourceConnected, source, occurredAt)
}

func (s *Service) sourceUpdatedEvent(source entity.PackageSource, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.sourceEvent(packageEventSourceUpdated, source, occurredAt)
}

func (s *Service) sourceDisabledEvent(source entity.PackageSource, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.sourceEvent(packageEventSourceDisabled, source, occurredAt)
}

func (s *Service) sourceEvent(eventType string, source entity.PackageSource, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(eventType, packageAggregateSource, source.ID, value.PackageEventPayload{
		SourceID:   source.ID.String(),
		SourceKind: string(source.Kind),
		Status:     string(source.Status),
		Version:    source.Version,
		Slug:       source.Slug,
	}, occurredAt)
}
