package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type verificationCommandPayload struct {
	VerificationID     string `json:"verification_id"`
	PackageVersionID   string `json:"package_version_id"`
	VerificationStatus string `json:"verification_status"`
	VerifiedByActorRef string `json:"verified_by_actor_ref"`
	VerificationNotes  string `json:"verification_notes"`
	ReleaseStatus      string `json:"release_status"`
	CreatedAt          string `json:"created_at"`
}

func (s *Service) findVerificationReplay(ctx context.Context, meta value.CommandMeta, packageVersionID uuid.UUID) (SetPackageVerificationResult, bool, error) {
	identity, err := commandIdentity(meta, packageOperationVerify)
	if err != nil {
		return SetPackageVerificationResult{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	if errors.Is(err, errs.ErrNotFound) {
		return SetPackageVerificationResult{}, false, nil
	}
	if err != nil {
		return SetPackageVerificationResult{}, false, err
	}
	if result.Operation != packageOperationVerify || result.AggregateType != enum.CommandAggregateTypePackageVersion || result.AggregateID != packageVersionID {
		return SetPackageVerificationResult{}, true, errs.ErrConflict
	}
	version, err := s.repository.GetPackageVersion(ctx, result.AggregateID)
	if err != nil {
		return SetPackageVerificationResult{}, true, err
	}
	verification, err := verificationFromPayload(result.ResultPayload)
	if err != nil {
		return SetPackageVerificationResult{}, true, err
	}
	return SetPackageVerificationResult{Verification: verification, Version: version}, true, nil
}

func commandIdentity(meta value.CommandMeta, operation string) (query.CommandIdentity, error) {
	if meta.CommandID == uuid.Nil && strings.TrimSpace(meta.IdempotencyKey) == "" {
		return query.CommandIdentity{}, errs.ErrInvalidArgument
	}
	var commandID *uuid.UUID
	if meta.CommandID != uuid.Nil {
		commandID = &meta.CommandID
	}
	return query.CommandIdentity{CommandID: commandID, IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey), Operation: operation}, nil
}

func commandResult(meta value.CommandMeta, operation string, aggregateType enum.CommandAggregateType, aggregateID uuid.UUID, payload []byte, now time.Time) (entity.CommandResult, error) {
	if meta.CommandID == uuid.Nil && strings.TrimSpace(meta.IdempotencyKey) == "" {
		return entity.CommandResult{}, errs.ErrInvalidArgument
	}
	key := operation + ":" + meta.CommandID.String()
	var commandID *uuid.UUID
	if meta.CommandID != uuid.Nil {
		commandID = &meta.CommandID
	} else {
		key = operation + ":" + strings.TrimSpace(meta.IdempotencyKey)
	}
	return entity.CommandResult{
		Key:            key,
		CommandID:      commandID,
		IdempotencyKey: strings.TrimSpace(meta.IdempotencyKey),
		Operation:      operation,
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
		ResultPayload:  payload,
		CreatedAt:      now,
	}, nil
}

func expectedRevision(meta value.CommandMeta) (int64, error) {
	if meta.ExpectedVersion == nil || *meta.ExpectedVersion < 1 {
		return 0, errs.ErrInvalidArgument
	}
	return *meta.ExpectedVersion, nil
}

func verificationPayload(verification entity.PackageVerification, version entity.PackageVersion) ([]byte, error) {
	return json.Marshal(verificationCommandPayload{
		VerificationID:     verification.ID.String(),
		PackageVersionID:   verification.PackageVersionID.String(),
		VerificationStatus: string(verification.VerificationStatus),
		VerifiedByActorRef: verification.VerifiedByActorRef,
		VerificationNotes:  verification.VerificationNotes,
		ReleaseStatus:      string(version.ReleaseStatus),
		CreatedAt:          verification.CreatedAt.Format(time.RFC3339Nano),
	})
}

func verificationFromPayload(payload []byte) (entity.PackageVerification, error) {
	var value verificationCommandPayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return entity.PackageVerification{}, errs.ErrInvalidArgument
	}
	verificationID, err := uuid.Parse(value.VerificationID)
	if err != nil || verificationID == uuid.Nil {
		return entity.PackageVerification{}, errs.ErrInvalidArgument
	}
	versionID, err := uuid.Parse(value.PackageVersionID)
	if err != nil || versionID == uuid.Nil {
		return entity.PackageVerification{}, errs.ErrInvalidArgument
	}
	createdAt, err := time.Parse(time.RFC3339Nano, value.CreatedAt)
	if err != nil {
		return entity.PackageVerification{}, errs.ErrInvalidArgument
	}
	return entity.PackageVerification{
		ID:                 verificationID,
		PackageVersionID:   versionID,
		VerificationStatus: enum.PackageVerificationStatus(value.VerificationStatus),
		VerifiedByActorRef: value.VerifiedByActorRef,
		VerificationNotes:  value.VerificationNotes,
		CreatedAt:          createdAt,
	}, nil
}
