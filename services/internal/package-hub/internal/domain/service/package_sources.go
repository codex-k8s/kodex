package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func (s *Service) ConnectPackageSource(ctx context.Context, input ConnectPackageSourceInput) (entity.PackageSource, error) {
	if err := requireOptionalID(input.OrganizationID); err != nil {
		return entity.PackageSource{}, err
	}
	if err := validateSourceIdentity(input.Slug, input.DisplayName, input.Kind); err != nil {
		return entity.PackageSource{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionSourceConnect, listSourcesResource(input.OrganizationID)); err != nil {
		return entity.PackageSource{}, err
	}
	replay, ok, err := s.findSourceReplay(ctx, input.Meta, packageOperationSourceConnect, uuid.Nil)
	if err != nil {
		return replay, err
	}
	if ok {
		if err := s.authorizeCommand(ctx, input.Meta, packageActionSourceRead, sourceResource(replay)); err != nil {
			return entity.PackageSource{}, err
		}
		return replay, nil
	}

	now := s.clock.Now()
	source := entity.PackageSource{
		VersionedBase: entity.VersionedBase{
			ID:        s.ids.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		OrganizationID:     input.OrganizationID,
		Slug:               strings.TrimSpace(input.Slug),
		DisplayName:        strings.TrimSpace(input.DisplayName),
		Kind:               input.Kind,
		RepositoryRef:      strings.TrimSpace(input.RepositoryRef),
		CatalogEndpointRef: strings.TrimSpace(input.CatalogEndpointRef),
		Status:             enum.PackageSourceStatusActive,
	}
	result, event, err := s.sourceCommandArtifacts(input.Meta, packageOperationSourceConnect, source, s.sourceConnectedEvent)
	if err != nil {
		return entity.PackageSource{}, err
	}
	if err := s.repository.CreatePackageSourceWithResult(ctx, source, result, event); err != nil {
		return entity.PackageSource{}, err
	}
	return source, nil
}

func (s *Service) UpdatePackageSource(ctx context.Context, input UpdatePackageSourceInput) (entity.PackageSource, error) {
	if err := requireID(input.SourceID); err != nil {
		return entity.PackageSource{}, err
	}
	current, err := s.repository.GetPackageSource(ctx, input.SourceID)
	if err != nil {
		return entity.PackageSource{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionSourceUpdate, sourceResource(current)); err != nil {
		return entity.PackageSource{}, err
	}
	replay, ok, err := s.findSourceReplay(ctx, input.Meta, packageOperationSourceUpdate, input.SourceID)
	if err != nil || ok {
		return replay, err
	}
	previousVersion, err := expectedRevision(input.Meta)
	if err != nil {
		return entity.PackageSource{}, err
	}
	updated, err := applyPackageSourceUpdate(current, input, s.clock.Now())
	if err != nil {
		return entity.PackageSource{}, err
	}
	result, event, err := s.sourceCommandArtifacts(input.Meta, packageOperationSourceUpdate, updated, s.sourceUpdatedEvent)
	if err != nil {
		return entity.PackageSource{}, err
	}
	if err := s.repository.UpdatePackageSourceWithResult(ctx, updated, previousVersion, result, event); err != nil {
		return entity.PackageSource{}, err
	}
	return updated, nil
}

func (s *Service) DisablePackageSource(ctx context.Context, input DisablePackageSourceInput) (entity.PackageSource, error) {
	if err := requireID(input.SourceID); err != nil {
		return entity.PackageSource{}, err
	}
	current, err := s.repository.GetPackageSource(ctx, input.SourceID)
	if err != nil {
		return entity.PackageSource{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, packageActionSourceDisable, sourceResource(current)); err != nil {
		return entity.PackageSource{}, err
	}
	replay, ok, err := s.findSourceReplay(ctx, input.Meta, packageOperationSourceDisable, input.SourceID)
	if err != nil || ok {
		return replay, err
	}
	previousVersion, err := expectedRevision(input.Meta)
	if err != nil {
		return entity.PackageSource{}, err
	}

	updated := current
	updated.Status = enum.PackageSourceStatusDisabled
	updated.Version = current.Version + 1
	updated.UpdatedAt = s.clock.Now()
	result, event, err := s.sourceCommandArtifacts(input.Meta, packageOperationSourceDisable, updated, s.sourceDisabledEvent)
	if err != nil {
		return entity.PackageSource{}, err
	}
	if err := s.repository.UpdatePackageSourceWithResult(ctx, updated, previousVersion, result, event); err != nil {
		return entity.PackageSource{}, err
	}
	return updated, nil
}

func validateSourceIdentity(slug string, displayName string, kind enum.PackageSourceKind) error {
	if err := requireText(slug); err != nil {
		return err
	}
	if err := requireText(displayName); err != nil {
		return err
	}
	return requireSourceKind(kind)
}

func applyPackageSourceUpdate(current entity.PackageSource, input UpdatePackageSourceInput, now time.Time) (entity.PackageSource, error) {
	if input.DisplayName == nil && input.RepositoryRef == nil && input.CatalogEndpointRef == nil && input.Status == nil {
		return entity.PackageSource{}, errs.ErrInvalidArgument
	}
	updated := current
	if input.DisplayName != nil {
		if err := requireText(*input.DisplayName); err != nil {
			return entity.PackageSource{}, err
		}
		updated.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.RepositoryRef != nil {
		updated.RepositoryRef = strings.TrimSpace(*input.RepositoryRef)
	}
	if input.CatalogEndpointRef != nil {
		updated.CatalogEndpointRef = strings.TrimSpace(*input.CatalogEndpointRef)
	}
	if input.Status != nil {
		if err := requireSourceUpdateStatus(*input.Status); err != nil {
			return entity.PackageSource{}, err
		}
		updated.Status = *input.Status
	}
	updated.Version = current.Version + 1
	updated.UpdatedAt = now
	return updated, nil
}

type sourceEventBuilder func(entity.PackageSource, time.Time) (entity.OutboxEvent, error)

func (s *Service) sourceCommandArtifacts(meta value.CommandMeta, operation string, source entity.PackageSource, eventBuilder sourceEventBuilder) (entity.CommandResult, entity.OutboxEvent, error) {
	payload, err := sourcePayload(source)
	if err != nil {
		return entity.CommandResult{}, entity.OutboxEvent{}, err
	}
	result, err := commandResult(meta, operation, enum.CommandAggregateTypePackageSource, source.ID, payload, source.UpdatedAt)
	if err != nil {
		return entity.CommandResult{}, entity.OutboxEvent{}, err
	}
	event, err := eventBuilder(source, source.UpdatedAt)
	if err != nil {
		return entity.CommandResult{}, entity.OutboxEvent{}, err
	}
	return result, event, nil
}
