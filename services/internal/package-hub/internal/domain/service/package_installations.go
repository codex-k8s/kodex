package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
)

func (s *Service) RequestPackageInstallation(ctx context.Context, input RequestPackageInstallationInput) (entity.PackageInstallation, error) {
	if err := validateInstallationRequest(input); err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionInstall, scopedInstallationResource("", input.Scope)); err != nil {
		return entity.PackageInstallation{}, err
	}
	replay, ok, err := findCommandReplayByType(ctx, s, input.Meta, replayByTypeSpec(packageOperationInstall, enum.CommandAggregateTypeInstallation, installationResultFromPayload))
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if ok {
		if err := requireInstallationReplay(input, replay); err != nil {
			return entity.PackageInstallation{}, err
		}
		if err := s.authorizeCommand(ctx, input.Meta, packageActionInstallationRead, installationResource(replay)); err != nil {
			return entity.PackageInstallation{}, err
		}
		return replay, nil
	}

	entry, version, err := s.installablePackageVersion(ctx, input.PackageID, input.PackageVersionID)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	requirements, err := s.packageVersionInstallationRequirements(ctx, version.ID)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	now := s.clock.Now()
	installation := entity.PackageInstallation{
		VersionedBase: entity.VersionedBase{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		PackageID:                entry.ID,
		PackageVersionID:         version.ID,
		Scope:                    input.Scope,
		InstallationStatus:       installationInitialStatus(requirements),
		DesiredState:             requestedDesiredState(input.DesiredState),
		RuntimeRequirementDigest: requirements.RuntimeRequirementDigest,
		SecretBindingStatus:      requirements.SecretBindingStatus,
		LastHealthStatus:         enum.PackageHealthStatusUnknown,
	}
	payload, err := installationPayload(installation)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	result, err := commandResult(input.Meta, packageOperationInstall, enum.CommandAggregateTypeInstallation, installation.ID, payload, now)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	event, err := s.installationCreatedEvent(installation, now)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.repository.CreatePackageInstallationWithResult(ctx, installation, result, event); err != nil {
		return entity.PackageInstallation{}, err
	}
	return installation, nil
}

func validateInstallationRequest(input RequestPackageInstallationInput) error {
	if err := requireID(input.PackageID); err != nil {
		return err
	}
	if err := requireID(input.PackageVersionID); err != nil {
		return err
	}
	if err := requireInstallationScope(input.Scope); err != nil {
		return err
	}
	if input.DesiredState == nil {
		return nil
	}
	if err := requireDesiredState(*input.DesiredState); err != nil {
		return err
	}
	if *input.DesiredState != enum.PackageDesiredStatePresent {
		return errs.ErrInvalidArgument
	}
	return nil
}

func (s *Service) installablePackageVersion(ctx context.Context, packageID uuid.UUID, packageVersionID uuid.UUID) (entity.PackageEntry, entity.PackageVersion, error) {
	entry, err := s.repository.GetPackage(ctx, packageID)
	if err != nil {
		return entity.PackageEntry{}, entity.PackageVersion{}, err
	}
	if !isInstallablePackage(entry) {
		return entity.PackageEntry{}, entity.PackageVersion{}, errs.ErrPreconditionFailed
	}
	version, err := s.repository.GetPackageVersion(ctx, packageVersionID)
	if err != nil {
		return entity.PackageEntry{}, entity.PackageVersion{}, err
	}
	if version.PackageID != entry.ID || !isInstallableVersion(version) {
		return entity.PackageEntry{}, entity.PackageVersion{}, errs.ErrPreconditionFailed
	}
	return entry, version, nil
}

func (s *Service) packageVersionInstallationRequirements(ctx context.Context, packageVersionID uuid.UUID) (packageInstallationRequirements, error) {
	manifest, err := s.repository.GetLatestManifestSnapshot(ctx, packageVersionID)
	if errors.Is(err, errs.ErrNotFound) {
		return packageInstallationRequirements{}, errs.ErrPreconditionFailed
	}
	if err != nil {
		return packageInstallationRequirements{}, err
	}
	return packageInstallationRequirementsFromManifest(manifest.Payload)
}

func isInstallablePackage(entry entity.PackageEntry) bool {
	return entry.Status == enum.PackageStatusAvailable && entry.TrustStatus != enum.PackageTrustStatusBlocked
}

func isInstallableVersion(version entity.PackageVersion) bool {
	if version.ReleaseStatus == enum.PackageReleaseStatusRevoked || version.ReleaseStatus == enum.PackageReleaseStatusBlocked {
		return false
	}
	return version.VerificationStatus != enum.PackageVerificationStatusRejected && version.VerificationStatus != enum.PackageVerificationStatusRevoked
}

func installationInitialStatus(requirements packageInstallationRequirements) enum.PackageInstallationStatus {
	if requirements.RuntimeRequirementDigest == "" && requirements.SecretBindingStatus != enum.PackageSecretBindingStatusMissing {
		return enum.PackageInstallationStatusActive
	}
	return enum.PackageInstallationStatusRequested
}

func requestedDesiredState(state *enum.PackageDesiredState) enum.PackageDesiredState {
	if state == nil {
		return enum.PackageDesiredStatePresent
	}
	return *state
}

func requireInstallationReplay(input RequestPackageInstallationInput, replay entity.PackageInstallation) error {
	if replay.PackageID != input.PackageID || replay.PackageVersionID != input.PackageVersionID {
		return errs.ErrConflict
	}
	if replay.Scope != input.Scope || replay.DesiredState != requestedDesiredState(input.DesiredState) {
		return errs.ErrConflict
	}
	return nil
}

func (s *Service) installationCreatedEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	if installation.InstallationStatus == enum.PackageInstallationStatusActive {
		return s.installationActivatedEvent(installation, occurredAt)
	}
	return s.installationRequestedEvent(installation, occurredAt)
}
