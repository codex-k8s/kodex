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

type sourceCommandPayload struct {
	Source packageSourceSnapshot `json:"source"`
}

type catalogSyncCommandPayload struct {
	Source       packageSourceSnapshot `json:"source"`
	PackageCount int64                 `json:"package_count"`
	VersionCount int64                 `json:"version_count"`
	SyncedAt     string                `json:"synced_at"`
}

type packageSourceSnapshot struct {
	ID                 string `json:"id"`
	OrganizationID     string `json:"organization_id,omitempty"`
	Slug               string `json:"slug"`
	DisplayName        string `json:"display_name"`
	SourceKind         string `json:"source_kind"`
	RepositoryRef      string `json:"repository_ref,omitempty"`
	CatalogEndpointRef string `json:"catalog_endpoint_ref,omitempty"`
	Status             string `json:"status"`
	LastSyncAt         string `json:"last_sync_at,omitempty"`
	LastError          string `json:"last_error,omitempty"`
	Version            int64  `json:"version"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
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

func (s *Service) findSourceReplay(ctx context.Context, meta value.CommandMeta, operation string, sourceID uuid.UUID) (entity.PackageSource, bool, error) {
	identity, err := commandIdentity(meta, operation)
	if err != nil {
		return entity.PackageSource{}, false, err
	}
	result, err := s.repository.GetCommandResult(ctx, identity)
	if errors.Is(err, errs.ErrNotFound) {
		return entity.PackageSource{}, false, nil
	}
	if err != nil {
		return entity.PackageSource{}, false, err
	}
	if result.Operation != operation || result.AggregateType != enum.CommandAggregateTypePackageSource {
		return entity.PackageSource{}, true, errs.ErrConflict
	}
	if sourceID != uuid.Nil && result.AggregateID != sourceID {
		return entity.PackageSource{}, true, errs.ErrConflict
	}
	source, err := sourceResultFromPayload(result.ResultPayload)
	if err != nil {
		return entity.PackageSource{}, true, err
	}
	return source, true, nil
}

type commandReplaySpec[T any] struct {
	Operation     string
	AggregateType enum.CommandAggregateType
	AggregateID   uuid.UUID
	Decode        func([]byte) (T, error)
}

func replaySpec[T any](operation string, aggregateType enum.CommandAggregateType, aggregateID uuid.UUID, decode func([]byte) (T, error)) commandReplaySpec[T] {
	return commandReplaySpec[T]{Operation: operation, AggregateType: aggregateType, AggregateID: aggregateID, Decode: decode}
}

func findCommandReplay[T any](ctx context.Context, service *Service, meta value.CommandMeta, spec commandReplaySpec[T]) (T, bool, error) {
	var zero T
	identity, err := commandIdentity(meta, spec.Operation)
	if err != nil {
		return zero, false, err
	}
	result, err := service.repository.GetCommandResult(ctx, identity)
	if errors.Is(err, errs.ErrNotFound) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	if result.Operation != spec.Operation || result.AggregateType != spec.AggregateType || result.AggregateID != spec.AggregateID {
		return zero, true, errs.ErrConflict
	}
	replay, err := spec.Decode(result.ResultPayload)
	if err != nil {
		return zero, true, err
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

func sourcePayload(source entity.PackageSource) ([]byte, error) {
	return json.Marshal(sourceCommandPayload{
		Source: packageSourceSnapshotFromEntity(source),
	})
}

func sourceResultFromPayload(payload []byte) (entity.PackageSource, error) {
	var value sourceCommandPayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return entity.PackageSource{}, errs.ErrInvalidArgument
	}
	return packageSourceFromSnapshot(value.Source)
}

func catalogSyncPayload(result SyncAvailablePackagesResult) ([]byte, error) {
	return json.Marshal(catalogSyncCommandPayload{
		Source:       packageSourceSnapshotFromEntity(result.Source),
		PackageCount: result.PackageCount,
		VersionCount: result.VersionCount,
		SyncedAt:     result.SyncedAt.Format(time.RFC3339Nano),
	})
}

func catalogSyncResultFromPayload(payload []byte) (SyncAvailablePackagesResult, error) {
	var stored catalogSyncCommandPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		return SyncAvailablePackagesResult{}, errs.ErrInvalidArgument
	}
	source, err := packageSourceFromSnapshot(stored.Source)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	syncedAt, err := parseRequiredTime(stored.SyncedAt)
	if err != nil {
		return SyncAvailablePackagesResult{}, err
	}
	return SyncAvailablePackagesResult{Source: source, PackageCount: stored.PackageCount, VersionCount: stored.VersionCount, SyncedAt: syncedAt}, nil
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

func packageSourceFromSnapshot(snapshot packageSourceSnapshot) (entity.PackageSource, error) {
	id, err := parseRequiredUUID(snapshot.ID)
	if err != nil {
		return entity.PackageSource{}, err
	}
	organizationID, err := parseOptionalUUID(snapshot.OrganizationID)
	if err != nil {
		return entity.PackageSource{}, err
	}
	createdAt, err := parseRequiredTime(snapshot.CreatedAt)
	if err != nil {
		return entity.PackageSource{}, err
	}
	updatedAt, err := parseRequiredTime(snapshot.UpdatedAt)
	if err != nil {
		return entity.PackageSource{}, err
	}
	lastSyncAt, err := parseOptionalTime(snapshot.LastSyncAt)
	if err != nil {
		return entity.PackageSource{}, err
	}
	return entity.PackageSource{
		VersionedBase: entity.VersionedBase{
			ID:        id,
			Version:   snapshot.Version,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		},
		OrganizationID:     organizationID,
		Slug:               snapshot.Slug,
		DisplayName:        snapshot.DisplayName,
		Kind:               enum.PackageSourceKind(snapshot.SourceKind),
		RepositoryRef:      snapshot.RepositoryRef,
		CatalogEndpointRef: snapshot.CatalogEndpointRef,
		Status:             enum.PackageSourceStatus(snapshot.Status),
		LastSyncAt:         lastSyncAt,
		LastError:          snapshot.LastError,
	}, nil
}

func packageSourceSnapshotFromEntity(source entity.PackageSource) packageSourceSnapshot {
	return packageSourceSnapshot{
		ID:                 source.ID.String(),
		OrganizationID:     formatOptionalUUID(source.OrganizationID),
		Slug:               source.Slug,
		DisplayName:        source.DisplayName,
		SourceKind:         string(source.Kind),
		RepositoryRef:      source.RepositoryRef,
		CatalogEndpointRef: source.CatalogEndpointRef,
		Status:             string(source.Status),
		LastSyncAt:         formatOptionalTime(source.LastSyncAt),
		LastError:          source.LastError,
		Version:            source.Version,
		CreatedAt:          source.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt:          source.UpdatedAt.Format(time.RFC3339Nano),
	}
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

func parseOptionalUUID(raw string) (*uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	id, err := parseRequiredUUID(raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
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

func formatOptionalUUID(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
