package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type secretStatusFlags struct {
	requiredTotal       int
	requiredReady       int
	requiredMissing     bool
	requiredInvalid     bool
	requiredCheckFailed bool
	optionalNotReady    bool
}

// RefreshPackageInstallationSecretStatus recalculates secret readiness without reading secret values.
func (s *Service) RefreshPackageInstallationSecretStatus(ctx context.Context, input RefreshPackageInstallationSecretStatusInput) (entity.PackageInstallation, error) {
	if err := validateRefreshPackageInstallationSecretStatusInput(input); err != nil {
		return entity.PackageInstallation{}, err
	}
	current, err := s.repository.GetPackageInstallation(ctx, input.InstallationID)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionInstallationUpdate, installationResource(current)); err != nil {
		return entity.PackageInstallation{}, err
	}
	replay, ok, err := findCommandReplay(ctx, s, input.Meta, replaySpec(packageOperationInstallationSecrets, enum.CommandAggregateTypeInstallation, input.InstallationID, installationResultFromPayload))
	if err != nil || ok {
		return replay, err
	}
	previousVersion, err := expectedRevision(input.Meta)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	secretFields, err := s.packageInstallationSecretFields(ctx, current.PackageVersionID)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	secretStatus, err := s.refreshSecretBindingStatus(ctx, current, input.Meta, secretFields)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	updated := applySecretBindingStatus(current, secretStatus, s.clock.Now())
	result, event, err := commandArtifacts(input.Meta, packageOperationInstallationSecrets, enum.CommandAggregateTypeInstallation, updated.ID, updated, updated.UpdatedAt, installationPayload, s.installationUpdatedEvent)
	if err != nil {
		return entity.PackageInstallation{}, err
	}
	if err := s.repository.UpdatePackageInstallationWithResult(ctx, updated, previousVersion, result, event); err != nil {
		return entity.PackageInstallation{}, err
	}
	return updated, nil
}

func validateRefreshPackageInstallationSecretStatusInput(input RefreshPackageInstallationSecretStatusInput) error {
	return requireID(input.InstallationID)
}

func (s *Service) packageInstallationSecretFields(ctx context.Context, packageVersionID uuid.UUID) ([]value.PackageSecretField, error) {
	schema, err := s.repository.GetLatestPackageSecretSchema(ctx, packageVersionID)
	if errors.Is(err, errs.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return schema.Fields, nil
}

func (s *Service) refreshSecretBindingStatus(ctx context.Context, installation entity.PackageInstallation, meta value.CommandMeta, fields []value.PackageSecretField) (enum.PackageSecretBindingStatus, error) {
	if len(fields) == 0 {
		return enum.PackageSecretBindingStatusNotRequired, nil
	}
	if s.secretRefReader == nil || s.secretChecker == nil {
		return "", errs.ErrDependencyUnavailable
	}
	refs, err := s.secretRefReader.ListPackageInstallationSecretRefs(ctx, ListPackageInstallationSecretRefsInput{
		PackageInstallationID: installation.ID,
		InstallationScope:     installation.Scope,
		LogicalKeys:           secretFieldKeys(fields),
		Meta:                  meta,
	})
	if err != nil {
		return "", err
	}
	return s.evaluateSecretBindingStatus(ctx, fields, refs.SecretRefs), nil
}

func (s *Service) evaluateSecretBindingStatus(ctx context.Context, fields []value.PackageSecretField, refs []value.PackageInstallationSecretRef) enum.PackageSecretBindingStatus {
	byKey := make(map[string]value.PackageInstallationSecretRef, len(refs))
	for _, ref := range refs {
		byKey[ref.LogicalKey] = ref
	}
	var flags secretStatusFlags
	for _, field := range fields {
		if field.Required {
			flags.requiredTotal++
		}
		state := s.evaluateOneSecretRef(ctx, byKey[field.Key])
		if field.Required {
			applyRequiredSecretState(&flags, state)
			continue
		}
		if state != enum.PackageSecretBindingStatusComplete {
			flags.optionalNotReady = true
		}
	}
	return summarizeSecretStatus(flags)
}

func (s *Service) evaluateOneSecretRef(ctx context.Context, ref value.PackageInstallationSecretRef) enum.PackageSecretBindingStatus {
	switch ref.Status {
	case enum.PackageInstallationSecretRefStatusConfigured:
		status, err := s.secretChecker.Check(ctx, secretresolver.SecretRef{StoreType: ref.StoreType, StoreRef: ref.StoreRef})
		if err == nil && status.Present {
			return enum.PackageSecretBindingStatusComplete
		}
		return secretStatusFromCheckError(err)
	case enum.PackageInstallationSecretRefStatusInvalid:
		return enum.PackageSecretBindingStatusInvalid
	case enum.PackageInstallationSecretRefStatusDisabled, enum.PackageInstallationSecretRefStatusMissing, "":
		return enum.PackageSecretBindingStatusMissing
	default:
		return enum.PackageSecretBindingStatusInvalid
	}
}

func applyRequiredSecretState(flags *secretStatusFlags, state enum.PackageSecretBindingStatus) {
	switch state {
	case enum.PackageSecretBindingStatusComplete:
		flags.requiredReady++
	case enum.PackageSecretBindingStatusMissing:
		flags.requiredMissing = true
	case enum.PackageSecretBindingStatusInvalid:
		flags.requiredInvalid = true
	case enum.PackageSecretBindingStatusCheckFailed:
		flags.requiredCheckFailed = true
	default:
		flags.requiredInvalid = true
	}
}

func summarizeSecretStatus(flags secretStatusFlags) enum.PackageSecretBindingStatus {
	switch {
	case flags.requiredMissing:
		return enum.PackageSecretBindingStatusMissing
	case flags.requiredInvalid:
		return enum.PackageSecretBindingStatusInvalid
	case flags.requiredCheckFailed:
		return enum.PackageSecretBindingStatusCheckFailed
	case flags.requiredTotal == 0 && flags.optionalNotReady:
		return enum.PackageSecretBindingStatusPartial
	case flags.requiredReady == flags.requiredTotal && flags.optionalNotReady:
		return enum.PackageSecretBindingStatusPartial
	default:
		return enum.PackageSecretBindingStatusComplete
	}
}

func secretStatusFromCheckError(err error) enum.PackageSecretBindingStatus {
	switch {
	case err == nil:
		return enum.PackageSecretBindingStatusMissing
	case errors.Is(err, secretresolver.ErrSecretNotFound):
		return enum.PackageSecretBindingStatusMissing
	case errors.Is(err, secretresolver.ErrInvalidRef):
		return enum.PackageSecretBindingStatusInvalid
	default:
		return enum.PackageSecretBindingStatusCheckFailed
	}
}

func applySecretBindingStatus(current entity.PackageInstallation, status enum.PackageSecretBindingStatus, now time.Time) entity.PackageInstallation {
	updated := current
	updated.SecretBindingStatus = status
	if current.InstallationStatus == enum.PackageInstallationStatusRequested && current.RuntimeRequirementDigest == "" && secretStatusAllowsActivation(status) {
		updated.InstallationStatus = enum.PackageInstallationStatusActive
	}
	updated.Version = current.Version + 1
	updated.UpdatedAt = now
	return updated
}

func secretStatusAllowsActivation(status enum.PackageSecretBindingStatus) bool {
	switch status {
	case enum.PackageSecretBindingStatusNotRequired, enum.PackageSecretBindingStatusComplete, enum.PackageSecretBindingStatusPartial:
		return true
	default:
		return false
	}
}

func secretFieldKeys(fields []value.PackageSecretField) []string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	return keys
}
