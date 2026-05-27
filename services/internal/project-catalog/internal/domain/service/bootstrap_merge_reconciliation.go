package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
)

const (
	bootstrapMergeSignalKind        = "bootstrap"
	bootstrapMergeIdempotencyPrefix = "provider-bootstrap-merge:"
)

type normalizedBootstrapMergeReconciliation struct {
	SignalKey                    string
	ProviderTarget               RepositoryBootstrapProviderTarget
	BaseBranch                   string
	SourceRef                    string
	MergeCommitSHA               string
	SourceBlobSHA                string
	WatermarkJSON                []byte
	ProviderWorkItemProjectionID string
	ProviderWebURL               string
	ProviderObjectID             string
	MergeObservedAt              string
	SourcePath                   string
	ContentHash                  string
	ValidatedPayload             []byte
}

// ReconcileBootstrapMergeSignal imports checked services.yaml from a safe provider bootstrap merge signal.
func (s *Service) ReconcileBootstrapMergeSignal(ctx context.Context, input ReconcileBootstrapMergeSignalInput) (BootstrapServicesPolicyImportResult, error) {
	normalized, err := normalizeBootstrapMergeReconciliationInput(input)
	if err != nil {
		return BootstrapServicesPolicyImportResult{}, err
	}
	meta := input.Meta
	if meta.CommandID == uuid.Nil && strings.TrimSpace(meta.IdempotencyKey) == "" {
		meta.IdempotencyKey = bootstrapMergeIdempotencyPrefix + normalized.SignalKey
	}
	return s.ImportBootstrapServicesPolicy(ctx, ImportBootstrapServicesPolicyInput{
		ProjectID:                    input.ProjectID,
		RepositoryID:                 input.RepositoryID,
		ProviderTarget:               normalized.ProviderTarget,
		BaseBranch:                   normalized.BaseBranch,
		SourceRef:                    normalized.SourceRef,
		SourceCommitSHA:              normalized.MergeCommitSHA,
		SourceBlobSHA:                normalized.SourceBlobSHA,
		SourcePath:                   normalized.SourcePath,
		ContentHash:                  normalized.ContentHash,
		ValidatedPayload:             normalized.ValidatedPayload,
		WatermarkJSON:                normalized.WatermarkJSON,
		ProviderWorkItemProjectionID: normalized.ProviderWorkItemProjectionID,
		ProviderWebURL:               normalized.ProviderWebURL,
		ProviderObjectID:             normalized.ProviderObjectID,
		MergeObservedAt:              normalized.MergeObservedAt,
		Meta:                         meta,
	})
}

func normalizeBootstrapMergeReconciliationInput(input ReconcileBootstrapMergeSignalInput) (normalizedBootstrapMergeReconciliation, error) {
	if err := requireProjectID(input.ProjectID); err != nil {
		return normalizedBootstrapMergeReconciliation{}, err
	}
	if input.RepositoryID == uuid.Nil {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrInvalidArgument
	}
	signal := input.MergeSignal
	policy := input.CheckedPolicy
	if err := validateOptionalSignalID(signal.SignalID); err != nil {
		return normalizedBootstrapMergeReconciliation{}, err
	}
	signalKey := strings.TrimSpace(signal.SignalKey)
	if signalKey == "" || strings.TrimSpace(signal.SignalKind) != bootstrapMergeSignalKind {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrInvalidArgument
	}
	baseBranch := normalizeBootstrapMergeBaseBranch(signal.BaseBranch)
	providerSourceRef := strings.TrimSpace(signal.SourceRef)
	mergeCommitSHA := strings.ToLower(strings.TrimSpace(signal.MergeCommitSHA))
	if baseBranch == "" || !validSafeProviderSourceRef(providerSourceRef) || !validGitCommitSHA(mergeCommitSHA) {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrInvalidArgument
	}
	sourceRef := "refs/heads/" + baseBranch
	sourcePath := strings.TrimSpace(policy.SourcePath)
	contentHash, err := normalizeSHA256Digest(policy.ContentHash)
	if err != nil {
		return normalizedBootstrapMergeReconciliation{}, err
	}
	artifactDigest, err := normalizeSHA256Digest(policy.ArtifactDigest)
	if err != nil {
		return normalizedBootstrapMergeReconciliation{}, err
	}
	if strings.TrimSpace(policy.ArtifactRef) == "" {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrInvalidArgument
	}
	if strings.ToLower(strings.TrimSpace(policy.ArtifactVersion)) != mergeCommitSHA {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrPreconditionFailed
	}
	if artifactDigest != contentHash {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrPreconditionFailed
	}
	payload := []byte(strings.TrimSpace(string(policy.ValidatedPayload)))
	if sourcePath != "services.yaml" || len(payload) == 0 || !json.Valid(payload) {
		return normalizedBootstrapMergeReconciliation{}, errs.ErrInvalidArgument
	}
	watermarkJSON, err := normalizeBootstrapWatermark(signal.WatermarkJSON)
	if err != nil {
		return normalizedBootstrapMergeReconciliation{}, err
	}
	if err := validateWatermarkDigest(signal.WatermarkDigest, watermarkJSON); err != nil {
		return normalizedBootstrapMergeReconciliation{}, err
	}
	if strings.TrimSpace(signal.MergeObservedAt) != "" {
		if _, err := parseRFC3339(signal.MergeObservedAt); err != nil {
			return normalizedBootstrapMergeReconciliation{}, err
		}
	}
	if strings.TrimSpace(signal.MergedAt) != "" {
		if _, err := parseRFC3339(signal.MergedAt); err != nil {
			return normalizedBootstrapMergeReconciliation{}, err
		}
	}
	return normalizedBootstrapMergeReconciliation{
		SignalKey:                    signalKey,
		ProviderTarget:               signal.ProviderTarget,
		BaseBranch:                   baseBranch,
		SourceRef:                    sourceRef,
		MergeCommitSHA:               mergeCommitSHA,
		SourceBlobSHA:                strings.TrimSpace(signal.SourceBlobSHA),
		WatermarkJSON:                watermarkJSON,
		ProviderWorkItemProjectionID: strings.TrimSpace(signal.ProviderWorkItemProjectionID),
		ProviderWebURL:               strings.TrimSpace(signal.ProviderWebURL),
		ProviderObjectID:             strings.TrimSpace(signal.ProviderObjectID),
		MergeObservedAt:              firstNonEmpty(strings.TrimSpace(signal.MergeObservedAt), strings.TrimSpace(signal.MergedAt)),
		SourcePath:                   sourcePath,
		ContentHash:                  contentHash,
		ValidatedPayload:             payload,
	}, nil
}

func validateOptionalSignalID(text string) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if _, err := uuid.Parse(trimmed); err != nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func normalizeBootstrapMergeBaseBranch(text string) string {
	return strings.TrimPrefix(strings.TrimSpace(text), "refs/heads/")
}

func validSafeProviderSourceRef(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > 256 {
		return false
	}
	return !strings.ContainsAny(trimmed, "\x00\r\n")
}

func normalizeSHA256Digest(text string) (string, error) {
	digest, err := normalizeSHA256HexDigest(text)
	if err != nil {
		return "", err
	}
	return "sha256:" + digest, nil
}

func normalizeSHA256HexDigest(text string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	trimmed = strings.TrimPrefix(trimmed, "sha256:")
	if len(trimmed) != sha256.Size*2 {
		return "", errs.ErrInvalidArgument
	}
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", errs.ErrInvalidArgument
	}
	return trimmed, nil
}

func validateWatermarkDigest(expected string, watermarkJSON []byte) error {
	digest, err := normalizeSHA256HexDigest(expected)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(watermarkJSON))))
	if digest != hex.EncodeToString(sum[:]) {
		return errs.ErrPreconditionFailed
	}
	return nil
}
