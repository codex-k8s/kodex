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
	Verification packageVerificationSnapshot `json:"verification"`
	Version      packageVersionSnapshot      `json:"version"`
}

type packageVerificationSnapshot struct {
	ID                 string `json:"id"`
	PackageVersionID   string `json:"package_version_id"`
	VerificationStatus string `json:"verification_status"`
	VerifiedByActorRef string `json:"verified_by_actor_ref"`
	VerificationNotes  string `json:"verification_notes"`
	CreatedAt          string `json:"created_at"`
}

type packageVersionSnapshot struct {
	ID                 string `json:"id"`
	PackageID          string `json:"package_id"`
	VersionLabel       string `json:"version_label"`
	SourceRefKind      string `json:"source_ref_kind"`
	SourceRef          string `json:"source_ref"`
	SourceCommitSHA    string `json:"source_commit_sha"`
	ManifestDigest     string `json:"manifest_digest"`
	VerificationStatus string `json:"verification_status"`
	ReleaseStatus      string `json:"release_status"`
	Revision           int64  `json:"revision"`
	PublishedAt        string `json:"published_at,omitempty"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
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
	replay, err := verificationResultFromPayload(result.ResultPayload)
	if err != nil {
		return SetPackageVerificationResult{}, true, err
	}
	return replay, true, nil
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
		Verification: packageVerificationSnapshot{
			ID:                 verification.ID.String(),
			PackageVersionID:   verification.PackageVersionID.String(),
			VerificationStatus: string(verification.VerificationStatus),
			VerifiedByActorRef: verification.VerifiedByActorRef,
			VerificationNotes:  verification.VerificationNotes,
			CreatedAt:          verification.CreatedAt.Format(time.RFC3339Nano),
		},
		Version: packageVersionSnapshot{
			ID:                 version.ID.String(),
			PackageID:          version.PackageID.String(),
			VersionLabel:       version.VersionLabel,
			SourceRefKind:      string(version.SourceRef.Kind),
			SourceRef:          version.SourceRef.Ref,
			SourceCommitSHA:    version.SourceRef.CommitSHA,
			ManifestDigest:     version.ManifestDigest,
			VerificationStatus: string(version.VerificationStatus),
			ReleaseStatus:      string(version.ReleaseStatus),
			Revision:           version.Revision,
			PublishedAt:        formatOptionalTime(version.PublishedAt),
			CreatedAt:          version.CreatedAt.Format(time.RFC3339Nano),
			UpdatedAt:          version.UpdatedAt.Format(time.RFC3339Nano),
		},
	})
}

func verificationResultFromPayload(payload []byte) (SetPackageVerificationResult, error) {
	var value verificationCommandPayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return SetPackageVerificationResult{}, errs.ErrInvalidArgument
	}
	verification, err := verificationFromSnapshot(value.Verification)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	version, err := packageVersionFromSnapshot(value.Version)
	if err != nil {
		return SetPackageVerificationResult{}, err
	}
	return SetPackageVerificationResult{Verification: verification, Version: version}, nil
}

func verificationFromSnapshot(snapshot packageVerificationSnapshot) (entity.PackageVerification, error) {
	verificationID, err := parseRequiredUUID(snapshot.ID)
	if err != nil {
		return entity.PackageVerification{}, err
	}
	versionID, err := parseRequiredUUID(snapshot.PackageVersionID)
	if err != nil {
		return entity.PackageVerification{}, err
	}
	createdAt, err := parseRequiredTime(snapshot.CreatedAt)
	if err != nil {
		return entity.PackageVerification{}, err
	}
	return entity.PackageVerification{
		ID:                 verificationID,
		PackageVersionID:   versionID,
		VerificationStatus: enum.PackageVerificationStatus(snapshot.VerificationStatus),
		VerifiedByActorRef: snapshot.VerifiedByActorRef,
		VerificationNotes:  snapshot.VerificationNotes,
		CreatedAt:          createdAt,
	}, nil
}

func packageVersionFromSnapshot(snapshot packageVersionSnapshot) (entity.PackageVersion, error) {
	versionID, err := parseRequiredUUID(snapshot.ID)
	if err != nil {
		return entity.PackageVersion{}, err
	}
	packageID, err := parseRequiredUUID(snapshot.PackageID)
	if err != nil {
		return entity.PackageVersion{}, err
	}
	createdAt, err := parseRequiredTime(snapshot.CreatedAt)
	if err != nil {
		return entity.PackageVersion{}, err
	}
	updatedAt, err := parseRequiredTime(snapshot.UpdatedAt)
	if err != nil {
		return entity.PackageVersion{}, err
	}
	publishedAt, err := parseOptionalTime(snapshot.PublishedAt)
	if err != nil {
		return entity.PackageVersion{}, err
	}
	return entity.PackageVersion{
		ID:           versionID,
		PackageID:    packageID,
		VersionLabel: snapshot.VersionLabel,
		SourceRef: value.SourceRef{
			Kind:      enum.PackageVersionSourceRefKind(snapshot.SourceRefKind),
			Ref:       snapshot.SourceRef,
			CommitSHA: snapshot.SourceCommitSHA,
		},
		ManifestDigest:     snapshot.ManifestDigest,
		VerificationStatus: enum.PackageVerificationStatus(snapshot.VerificationStatus),
		ReleaseStatus:      enum.PackageReleaseStatus(snapshot.ReleaseStatus),
		Revision:           snapshot.Revision,
		PublishedAt:        publishedAt,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

func parseRequiredUUID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func parseRequiredTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return parsed, nil
}

func parseOptionalTime(raw string) (*time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := parseRequiredTime(raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}
