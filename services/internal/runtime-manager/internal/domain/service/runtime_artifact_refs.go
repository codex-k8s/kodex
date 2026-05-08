package service

import (
	"bytes"
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
)

const aggregateTypeArtifactRef = "runtime_artifact_ref"

// RecordRuntimeArtifactRef stores one safe pointer to an external runtime artifact.
func (s *Service) RecordRuntimeArtifactRef(ctx context.Context, input RecordRuntimeArtifactRefInput) (entity.RuntimeArtifactRef, error) {
	if err := validateRecordRuntimeArtifactRefInput(input); err != nil {
		return entity.RuntimeArtifactRef{}, err
	}
	projectID, err := s.artifactProjectScope(ctx, input.JobID, input.SlotID)
	if err != nil {
		return entity.RuntimeArtifactRef{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionArtifactRecord, artifactResource(uuid.Nil, projectID)); err != nil {
		return entity.RuntimeArtifactRef{}, err
	}
	if replay, ok, err := aggregateReplay(ctx, input.Meta, operationRecordArtifact, aggregateTypeArtifactRef, nil, s.findCommandResult, s.repository.GetRuntimeArtifactRef); err != nil || ok {
		if err == nil {
			err = validateRuntimeArtifactReplayScope(replay, input)
		}
		return replay, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	refs, err := newRuntimeArtifactRefs(s.ids, []RuntimeArtifactRefInput{input.ArtifactRef}, input.JobID, input.SlotID, now)
	if err != nil {
		return entity.RuntimeArtifactRef{}, err
	}
	ref := refs[0]
	result, err := commandResult(input.Meta, operationRecordArtifact, aggregateTypeArtifactRef, ref.ID, nil, now)
	if err != nil {
		return entity.RuntimeArtifactRef{}, err
	}
	return ref, s.repository.RecordRuntimeArtifactRef(ctx, ref, result)
}

// ListRuntimeArtifactRefs returns external artifact references by job, slot or type.
func (s *Service) ListRuntimeArtifactRefs(ctx context.Context, input ListRuntimeArtifactRefsInput) (ListRuntimeArtifactRefsResult, error) {
	for _, artifactType := range input.ArtifactTypes {
		if !validRuntimeArtifactType(artifactType) {
			return ListRuntimeArtifactRefsResult{}, errs.ErrInvalidArgument
		}
	}
	projectID, err := s.artifactProjectScope(ctx, input.JobID, input.SlotID)
	if err != nil {
		return ListRuntimeArtifactRefsResult{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, actionArtifactList, artifactResource(uuid.Nil, projectID)); err != nil {
		return ListRuntimeArtifactRefsResult{}, err
	}
	filter := query.RuntimeArtifactRefFilter{
		JobID:         input.JobID,
		SlotID:        input.SlotID,
		ArtifactTypes: append([]enum.RuntimeArtifactType(nil), input.ArtifactTypes...),
		Page:          input.Page,
	}
	refs, page, err := s.repository.ListRuntimeArtifactRefs(ctx, filter)
	return ListRuntimeArtifactRefsResult{RuntimeArtifactRefs: refs, Page: page}, err
}

func validateRuntimeArtifactReplayScope(ref entity.RuntimeArtifactRef, input RecordRuntimeArtifactRefInput) error {
	metadata, err := normalizedJSONObject(input.ArtifactRef.MetadataJSON)
	if err != nil {
		return err
	}
	if !sameUUIDPtr(ref.JobID, input.JobID) ||
		!sameUUIDPtr(ref.SlotID, input.SlotID) ||
		ref.ArtifactType != input.ArtifactRef.ArtifactType ||
		ref.ExternalRef != strings.TrimSpace(input.ArtifactRef.ExternalRef) ||
		ref.Digest != strings.TrimSpace(input.ArtifactRef.Digest) ||
		!bytes.Equal(ref.MetadataJSON, metadata) {
		return errs.ErrConflict
	}
	return nil
}

func validateRecordRuntimeArtifactRefInput(input RecordRuntimeArtifactRefInput) error {
	if input.JobID == nil && input.SlotID == nil {
		return errs.ErrInvalidArgument
	}
	if err := validateRuntimeArtifactRefInput(input.ArtifactRef); err != nil {
		return err
	}
	_, err := commandIdentity(input.Meta, operationRecordArtifact)
	return err
}

func (s *Service) artifactProjectScope(ctx context.Context, jobID *uuid.UUID, slotID *uuid.UUID) (*uuid.UUID, error) {
	var projectID *uuid.UUID
	var jobSlotID *uuid.UUID
	if jobID != nil {
		job, err := s.repository.GetJob(ctx, *jobID)
		if err != nil {
			return nil, err
		}
		projectID = job.ProjectID
		jobSlotID = job.SlotID
	}
	if slotID != nil {
		slot, err := s.repository.GetSlot(ctx, *slotID)
		if err != nil {
			return nil, err
		}
		if jobSlotID != nil && *jobSlotID != *slotID {
			return nil, errs.ErrConflict
		}
		if projectID != nil && !sameUUIDPtr(projectID, slot.ProjectID) {
			return nil, errs.ErrConflict
		}
		projectID = slot.ProjectID
	}
	return projectID, nil
}
