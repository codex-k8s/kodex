package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
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

func (s *Service) UpdatePackageInstallation(ctx context.Context, input UpdatePackageInstallationInput) (entity.PackageInstallation, error) {
	if err := validatePackageInstallationUpdateInput(input); err != nil {
		return entity.PackageInstallation{}, err
	}
	current, err := s.repository.GetPackageInstallation(ctx, input.InstallationID)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionInstallationUpdate, installationResource(current)); err != nil {
		return entity.PackageInstallation{}, err
	}
	replay, ok, err := findCommandReplay(ctx, s, input.Meta, replaySpec(packageOperationInstallationUpdate, enum.CommandAggregateTypeInstallation, input.InstallationID, installationResultFromPayload))
	if err != nil || ok {
		return replay, err
	}
	previousVersion, err := expectedRevision(input.Meta)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	updated, err := s.applyPackageInstallationUpdate(ctx, current, input)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	result, event, err := commandArtifacts(input.Meta, packageOperationInstallationUpdate, enum.CommandAggregateTypeInstallation, updated.ID, updated, updated.UpdatedAt, installationPayload, s.installationUpdatedEvent)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.repository.UpdatePackageInstallationWithResult(ctx, updated, previousVersion, result, event); err != nil {
		return entity.PackageInstallation{}, err
	}
	return updated, nil
}

func (s *Service) DisablePackageInstallation(ctx context.Context, input DisablePackageInstallationInput) (entity.PackageInstallation, error) {
	return s.changePackageInstallationLifecycle(ctx, input.InstallationID, input.Meta, packageActionInstallationDisable, packageOperationInstallationDisable, s.disablePackageInstallation, s.installationDisabledEvent)
}

func (s *Service) UninstallPackage(ctx context.Context, input UninstallPackageInput) (entity.PackageInstallation, error) {
	return s.changePackageInstallationLifecycle(ctx, input.InstallationID, input.Meta, packageActionUninstall, packageOperationUninstall, s.uninstallPackageInstallation, s.installationUninstalledEvent)
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

func validatePackageInstallationUpdateInput(input UpdatePackageInstallationInput) error {
	if err := requireID(input.InstallationID); err != nil {
		return err
	}
	if input.PackageVersionID == nil && input.DesiredState == nil && input.InstallationStatus == nil {
		return errs.ErrInvalidArgument
	}
	if err := requireOptionalID(input.PackageVersionID); err != nil {
		return err
	}
	if input.DesiredState != nil {
		if err := requireDesiredState(*input.DesiredState); err != nil {
			return err
		}
	}
	if input.InstallationStatus != nil {
		if err := requirePackageInstallationUpdateStatus(*input.InstallationStatus); err != nil {
			return err
		}
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

func (s *Service) applyPackageInstallationUpdate(ctx context.Context, current entity.PackageInstallation, input UpdatePackageInstallationInput) (entity.PackageInstallation, error) {
	if current.InstallationStatus == enum.PackageInstallationStatusUninstalled {
		return entity.PackageInstallation{}, errs.ErrPreconditionFailed
	}
	updated := current
	changed := false

	if input.PackageVersionID != nil && *input.PackageVersionID != current.PackageVersionID {
		requirements, err := s.installationRequirementsForVersionChange(ctx, current.PackageID, *input.PackageVersionID)
		if err != nil {
			return entity.PackageInstallation{}, err
		}
		updated.PackageVersionID = *input.PackageVersionID
		updated.RuntimeRequirementDigest = requirements.RuntimeRequirementDigest
		updated.SecretBindingStatus = requirements.SecretBindingStatus
		updated.LastHealthStatus = enum.PackageHealthStatusUnknown
		if current.InstallationStatus != enum.PackageInstallationStatusDisabled {
			updated.InstallationStatus = installationInitialStatus(requirements)
		}
		changed = true
	}
	if input.DesiredState != nil && *input.DesiredState != updated.DesiredState {
		updated.DesiredState = *input.DesiredState
		changed = true
	}
	if input.InstallationStatus != nil && *input.InstallationStatus != updated.InstallationStatus {
		updated.InstallationStatus = *input.InstallationStatus
		changed = true
	}
	if !changed {
		return entity.PackageInstallation{}, errs.ErrInvalidArgument
	}
	if updated.InstallationStatus == enum.PackageInstallationStatusActive && !secretStatusAllowsActivation(updated.SecretBindingStatus) {
		return entity.PackageInstallation{}, errs.ErrPreconditionFailed
	}
	updated.Version = current.Version + 1
	updated.UpdatedAt = s.clock.Now()
	return updated, nil
}

func (s *Service) installationRequirementsForVersionChange(ctx context.Context, packageID uuid.UUID, packageVersionID uuid.UUID) (packageInstallationRequirements, error) {
	_, version, err := s.installablePackageVersion(ctx, packageID, packageVersionID)
	if err != nil {
		return packageInstallationRequirements{}, err
	}
	return s.packageVersionInstallationRequirements(ctx, version.ID)
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

func requirePackageInstallationUpdateStatus(status enum.PackageInstallationStatus) error {
	if status == enum.PackageInstallationStatusDisabled || status == enum.PackageInstallationStatusUninstalled {
		return errs.ErrInvalidArgument
	}
	return requireInstallationStatus(status)
}

func installationInitialStatus(requirements packageInstallationRequirements) enum.PackageInstallationStatus {
	if requirements.RuntimeRequirementDigest == "" && secretStatusAllowsActivation(requirements.SecretBindingStatus) {
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

type packageInstallationStateMutator func(entity.PackageInstallation, time.Time) (entity.PackageInstallation, error)

type packageInstallationEventBuilder func(entity.PackageInstallation, time.Time) (entity.OutboxEvent, error)

func (s *Service) changePackageInstallationLifecycle(
	ctx context.Context,
	installationID uuid.UUID,
	meta value.CommandMeta,
	actionKey string,
	operation string,
	mutate packageInstallationStateMutator,
	eventBuilder packageInstallationEventBuilder,
) (entity.PackageInstallation, error) {
	if err := requireID(installationID); err != nil {
		return entity.PackageInstallation{}, err
	}
	current, err := s.repository.GetPackageInstallation(ctx, installationID)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.authorizeCommand(ctx, meta, actionKey, installationResource(current)); err != nil {
		return entity.PackageInstallation{}, err
	}
	replay, ok, err := findCommandReplay(ctx, s, meta, replaySpec(operation, enum.CommandAggregateTypeInstallation, installationID, installationResultFromPayload))
	if err != nil || ok {
		return replay, err
	}
	previousVersion, err := expectedRevision(meta)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	updated, err := mutate(current, s.clock.Now())
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	result, event, err := commandArtifacts(meta, operation, enum.CommandAggregateTypeInstallation, updated.ID, updated, updated.UpdatedAt, installationPayload, eventBuilder)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.repository.UpdatePackageInstallationWithResult(ctx, updated, previousVersion, result, event); err != nil {
		return entity.PackageInstallation{}, err
	}
	return updated, nil
}

func (s *Service) disablePackageInstallation(current entity.PackageInstallation, now time.Time) (entity.PackageInstallation, error) {
	if current.InstallationStatus == enum.PackageInstallationStatusDisabled || current.InstallationStatus == enum.PackageInstallationStatusUninstalled {
		return entity.PackageInstallation{}, errs.ErrPreconditionFailed
	}
	updated := current
	updated.InstallationStatus = enum.PackageInstallationStatusDisabled
	updated.DesiredState = enum.PackageDesiredStateSuspended
	updated.LastHealthStatus = enum.PackageHealthStatusUnknown
	updated.Version = current.Version + 1
	updated.UpdatedAt = now
	return updated, nil
}

func (s *Service) uninstallPackageInstallation(current entity.PackageInstallation, now time.Time) (entity.PackageInstallation, error) {
	if current.InstallationStatus == enum.PackageInstallationStatusUninstalled {
		return entity.PackageInstallation{}, errs.ErrPreconditionFailed
	}
	updated := current
	updated.InstallationStatus = enum.PackageInstallationStatusUninstalled
	updated.DesiredState = enum.PackageDesiredStateAbsent
	updated.LastHealthStatus = enum.PackageHealthStatusUnknown
	updated.Version = current.Version + 1
	updated.UpdatedAt = now
	return updated, nil
}

func (s *Service) installationCreatedEvent(installation entity.PackageInstallation, occurredAt time.Time) (entity.OutboxEvent, error) {
	if installation.InstallationStatus == enum.PackageInstallationStatusActive {
		return s.installationActivatedEvent(installation, occurredAt)
	}
	return s.installationRequestedEvent(installation, occurredAt)
}
