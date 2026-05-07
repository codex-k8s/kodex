package service

import (
	"context"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
)

func (s *Service) SetPackageVerification(ctx context.Context, input SetPackageVerificationInput) (SetPackageVerificationResult, error) {
	if err := requireID(input.PackageVersionID); err != nil {
		return SetPackageVerificationResult{}, err
	}
	if err := requireVerificationStatus(input.VerificationStatus); err != nil {
		return SetPackageVerificationResult{}, err
	}
	replay, ok, err := s.findVerificationReplay(ctx, input.Meta, input.PackageVersionID)
	if err != nil || ok {
		return replay, err
	}
	previousRevision, err := expectedRevision(input.Meta)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	current, err := s.repository.GetPackageVersion(ctx, input.PackageVersionID)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	updated := current
	updated.VerificationStatus = input.VerificationStatus
	if input.ReleaseStatus != nil {
		updated.ReleaseStatus = *input.ReleaseStatus
	}
	updated.Revision = current.Revision + 1
	updated.UpdatedAt = s.clock.Now()

	verification := entity.PackageVerification{
		ID:                 s.ids.New(),
		PackageVersionID:   input.PackageVersionID,
		VerificationStatus: input.VerificationStatus,
		VerifiedByActorRef: defaultActorRef(input.Meta.Actor.Type, input.Meta.Actor.ID),
		VerificationNotes:  strings.TrimSpace(input.VerificationNotes),
		CreatedAt:          updated.UpdatedAt,
	}
	payload, err := verificationPayload(verification, updated)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	result, err := commandResult(input.Meta, packageOperationVerify, enum.CommandAggregateTypePackageVersion, input.PackageVersionID, payload, updated.UpdatedAt)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	event, err := s.verificationUpdatedEvent(updated, updated.UpdatedAt)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	if err := s.repository.SetPackageVerification(ctx, updated, previousRevision, verification, result, event); err != nil {
		return SetPackageVerificationResult{}, err
	}
	return SetPackageVerificationResult{Verification: verification, Version: updated}, nil
}
