package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

const (
	buildContextSourceSnapshotUnavailableCode    = "source_snapshot_unavailable"
	buildContextSourceSnapshotUnavailableMessage = "checked source snapshot is required before build context materialization"
	buildContextSourceSnapshotUnavailableAction  = "provide_checked_source_snapshot"
	buildContextMaterializerUnavailableCode      = "build_context_materializer_unavailable"
	buildContextMaterializerUnavailableMessage   = "build context materializer is not connected"
	buildContextMaterializerUnavailableAction    = "run_build_context_materializer"
	buildContextFailureAction                    = "review_build_context_materialization"
	maxBuildContextProviderBytes                 = 64
	maxBuildContextServiceKeys                   = 64
)

// PrepareBuildContext records or reuses a runtime-owned build context request.
func (s *Service) PrepareBuildContext(ctx context.Context, input PrepareBuildContextInput) (entity.BuildContext, error) {
	normalized, err := normalizePrepareBuildContextInput(input)
	if err != nil {
		return entity.BuildContext{}, err
	}
	if err := s.authorizeCommand(ctx, normalized.Meta, actionBuildContextPrepare, buildContextResource(uuid.Nil, &normalized.ProjectID)); err != nil {
		return entity.BuildContext{}, err
	}
	if replay, ok, err := aggregateReplay(ctx, normalized.Meta, operationPrepareBuildCtx, aggregateTypeBuildContext, nil, s.findCommandResult, s.repository.GetBuildContext); err != nil || ok {
		if err == nil {
			err = validateBuildContextReplayScope(replay, normalized)
		}
		return replay, err
	}
	now := commandTime(normalized.Meta, s.clock.Now())
	buildContext := newBuildContext(s.ids.New(), normalized, now)
	factory := func(stored entity.BuildContext) (entity.CommandResult, error) {
		return commandResult(normalized.Meta, operationPrepareBuildCtx, aggregateTypeBuildContext, stored.ID, nil, now)
	}
	return s.repository.PrepareBuildContext(ctx, buildContext, factory)
}

// ReportBuildContextProgress updates trusted build context materialization status.
func (s *Service) ReportBuildContextProgress(ctx context.Context, input ReportBuildContextProgressInput) (entity.BuildContext, error) {
	normalized, err := normalizeReportBuildContextProgressInput(input)
	if err != nil {
		return entity.BuildContext{}, err
	}
	buildContext, err := s.repository.GetBuildContext(ctx, normalized.BuildContextID)
	if err != nil {
		return entity.BuildContext{}, err
	}
	if err := s.authorizeCommand(ctx, normalized.Meta, actionBuildContextReport, buildContextResource(buildContext.ID, &buildContext.ProjectID)); err != nil {
		return entity.BuildContext{}, err
	}
	if replay, ok, err := aggregateReplay(ctx, normalized.Meta, operationReportBuildCtx, aggregateTypeBuildContext, &normalized.BuildContextID, s.findCommandResult, s.repository.GetBuildContext); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(normalized.Meta)
	if err != nil {
		return entity.BuildContext{}, err
	}
	if buildContext.Version != expected || terminalBuildContextStatus(buildContext.Status) {
		return entity.BuildContext{}, errs.ErrConflict
	}
	if err := validateBuildContextProgressScope(buildContext, normalized); err != nil {
		return entity.BuildContext{}, err
	}
	now := commandTime(normalized.Meta, s.clock.Now())
	updateBuildContextProgress(&buildContext, normalized, now)
	result, err := commandResult(normalized.Meta, operationReportBuildCtx, aggregateTypeBuildContext, buildContext.ID, nil, now)
	if err != nil {
		return entity.BuildContext{}, err
	}
	return buildContext, s.repository.UpdateBuildContext(ctx, buildContext, expected, result)
}

// GetBuildContext returns authoritative build context state.
func (s *Service) GetBuildContext(ctx context.Context, input GetBuildContextInput) (entity.BuildContext, error) {
	buildContext, err := s.loadBuildContext(ctx, input)
	if err != nil {
		return entity.BuildContext{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, actionBuildContextRead, buildContextResource(buildContext.ID, &buildContext.ProjectID)); err != nil {
		return entity.BuildContext{}, err
	}
	return buildContext, nil
}

func (s *Service) loadBuildContext(ctx context.Context, input GetBuildContextInput) (entity.BuildContext, error) {
	if input.BuildContextID != uuid.Nil {
		return s.repository.GetBuildContext(ctx, input.BuildContextID)
	}
	fingerprint := strings.TrimSpace(strings.ToLower(input.ContextFingerprint))
	if !validAgentRunSHA256Digest(fingerprint) {
		return entity.BuildContext{}, errs.ErrInvalidArgument
	}
	return s.repository.GetBuildContextByFingerprint(ctx, fingerprint)
}

func normalizePrepareBuildContextInput(input PrepareBuildContextInput) (PrepareBuildContextInput, error) {
	if input.ProjectID == uuid.Nil || input.RepositoryID == uuid.Nil {
		return PrepareBuildContextInput{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta, operationPrepareBuildCtx); err != nil {
		return PrepareBuildContextInput{}, err
	}
	normalized := PrepareBuildContextInput{
		ProjectID:            input.ProjectID,
		RepositoryID:         input.RepositoryID,
		Provider:             strings.TrimSpace(strings.ToLower(input.Provider)),
		ProviderOwner:        strings.TrimSpace(input.ProviderOwner),
		ProviderName:         strings.TrimSpace(input.ProviderName),
		SourceRef:            strings.TrimSpace(input.SourceRef),
		SourceCommitSHA:      strings.TrimSpace(strings.ToLower(input.SourceCommitSHA)),
		BuildPlanFingerprint: strings.TrimSpace(strings.ToLower(input.BuildPlanFingerprint)),
		SourceSnapshotRef:    strings.TrimSpace(input.SourceSnapshotRef),
		SourceSnapshotDigest: strings.TrimSpace(strings.ToLower(input.SourceSnapshotDigest)),
		Meta:                 input.Meta,
	}
	if !safeAgentRunLabel(normalized.Provider, maxBuildContextProviderBytes) ||
		!safeAgentRunRef(normalized.ProviderOwner, true) ||
		!safeAgentRunRef(normalized.ProviderName, true) ||
		!safeAgentRunRef(normalized.SourceRef, true) ||
		!validRuntimeJobCommitSHA(normalized.SourceCommitSHA) ||
		!validAgentRunSHA256Digest(normalized.BuildPlanFingerprint) {
		return PrepareBuildContextInput{}, errs.ErrInvalidArgument
	}
	keys, err := normalizeBuildContextServiceKeys(input.AffectedServiceKeys)
	if err != nil {
		return PrepareBuildContextInput{}, err
	}
	normalized.AffectedServiceKeys = keys
	if err := validateOptionalSourceSnapshot(normalized.SourceSnapshotRef, normalized.SourceSnapshotDigest); err != nil {
		return PrepareBuildContextInput{}, err
	}
	return normalized, nil
}

func normalizeReportBuildContextProgressInput(input ReportBuildContextProgressInput) (ReportBuildContextProgressInput, error) {
	if input.BuildContextID == uuid.Nil || !validBuildContextStatus(input.Status) {
		return ReportBuildContextProgressInput{}, errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta, operationReportBuildCtx); err != nil {
		return ReportBuildContextProgressInput{}, err
	}
	normalized := ReportBuildContextProgressInput{
		BuildContextID:       input.BuildContextID,
		Status:               input.Status,
		SourceSnapshotRef:    strings.TrimSpace(input.SourceSnapshotRef),
		SourceSnapshotDigest: strings.TrimSpace(strings.ToLower(input.SourceSnapshotDigest)),
		BuildContextRef:      strings.TrimSpace(input.BuildContextRef),
		BuildContextDigest:   strings.TrimSpace(strings.ToLower(input.BuildContextDigest)),
		StartedAt:            input.StartedAt,
		FinishedAt:           input.FinishedAt,
		ErrorCode:            strings.TrimSpace(input.ErrorCode),
		ErrorMessage:         strings.TrimSpace(input.ErrorMessage),
		NextAction:           strings.TrimSpace(input.NextAction),
		Meta:                 input.Meta,
	}
	if err := validateOptionalSourceSnapshot(normalized.SourceSnapshotRef, normalized.SourceSnapshotDigest); err != nil {
		return ReportBuildContextProgressInput{}, err
	}
	if normalized.BuildContextRef != "" || normalized.BuildContextDigest != "" {
		if !safeAgentRunRef(normalized.BuildContextRef, true) || !validAgentRunSHA256Digest(normalized.BuildContextDigest) {
			return ReportBuildContextProgressInput{}, errs.ErrInvalidArgument
		}
	}
	if normalized.Status == enum.BuildContextStatusReady && (normalized.BuildContextRef == "" || normalized.BuildContextDigest == "") {
		return ReportBuildContextProgressInput{}, errs.ErrInvalidArgument
	}
	if normalized.Status == enum.BuildContextStatusFailed && normalized.ErrorCode == "" {
		return ReportBuildContextProgressInput{}, errs.ErrInvalidArgument
	}
	return normalized, nil
}

func normalizeBuildContextServiceKeys(keys []string) ([]string, error) {
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		normalized := strings.TrimSpace(key)
		if normalized == "" {
			continue
		}
		if !safeAgentRunLabel(normalized, maxRuntimeJobServiceKeyBytes) {
			return nil, errs.ErrInvalidArgument
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) > maxBuildContextServiceKeys {
		return nil, errs.ErrInvalidArgument
	}
	sort.Strings(result)
	return result, nil
}

func validateOptionalSourceSnapshot(ref string, digest string) error {
	if ref == "" && digest == "" {
		return nil
	}
	if !safeAgentRunRef(ref, true) || !validAgentRunSHA256Digest(digest) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func newBuildContext(id uuid.UUID, input PrepareBuildContextInput, now time.Time) entity.BuildContext {
	buildContext := entity.BuildContext{
		Base: entity.Base{
			ID:        id,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Status:               enum.BuildContextStatusPending,
		ProjectID:            input.ProjectID,
		RepositoryID:         input.RepositoryID,
		Provider:             input.Provider,
		ProviderOwner:        input.ProviderOwner,
		ProviderName:         input.ProviderName,
		SourceRef:            input.SourceRef,
		SourceCommitSHA:      input.SourceCommitSHA,
		AffectedServiceKeys:  append([]string(nil), input.AffectedServiceKeys...),
		BuildPlanFingerprint: input.BuildPlanFingerprint,
		ContextFingerprint:   buildContextFingerprint(input),
		SourceSnapshotRef:    input.SourceSnapshotRef,
		SourceSnapshotDigest: input.SourceSnapshotDigest,
	}
	applyBuildContextPendingDiagnostic(&buildContext)
	return buildContext
}

func updateBuildContextProgress(buildContext *entity.BuildContext, input ReportBuildContextProgressInput, now time.Time) {
	buildContext.Status = input.Status
	if input.SourceSnapshotRef != "" {
		buildContext.SourceSnapshotRef = input.SourceSnapshotRef
		buildContext.SourceSnapshotDigest = input.SourceSnapshotDigest
	}
	if input.BuildContextRef != "" {
		buildContext.BuildContextRef = input.BuildContextRef
		buildContext.BuildContextDigest = input.BuildContextDigest
	}
	if input.StartedAt != nil {
		buildContext.StartedAt = input.StartedAt
	} else if input.Status == enum.BuildContextStatusRunning && buildContext.StartedAt == nil {
		buildContext.StartedAt = timePtr(now)
	}
	if input.FinishedAt != nil {
		buildContext.FinishedAt = input.FinishedAt
	} else if terminalBuildContextStatus(input.Status) && buildContext.FinishedAt == nil {
		buildContext.FinishedAt = timePtr(now)
	}
	buildContext.LastErrorCode = input.ErrorCode
	buildContext.LastErrorMessage = input.ErrorMessage
	buildContext.NextAction = input.NextAction
	switch input.Status {
	case enum.BuildContextStatusPending:
		applyBuildContextPendingDiagnostic(buildContext)
	case enum.BuildContextStatusReady:
		buildContext.LastErrorCode = ""
		buildContext.LastErrorMessage = ""
		buildContext.NextAction = ""
	case enum.BuildContextStatusFailed:
		if buildContext.NextAction == "" {
			buildContext.NextAction = buildContextFailureAction
		}
	}
	buildContext.UpdatedAt = now
	buildContext.Version++
}

func validateBuildContextProgressScope(buildContext entity.BuildContext, input ReportBuildContextProgressInput) error {
	sourceSnapshotRef := buildContext.SourceSnapshotRef
	sourceSnapshotDigest := buildContext.SourceSnapshotDigest
	if input.SourceSnapshotRef != "" {
		if sourceSnapshotRef != "" && (sourceSnapshotRef != input.SourceSnapshotRef || sourceSnapshotDigest != input.SourceSnapshotDigest) {
			return errs.ErrConflict
		}
		sourceSnapshotRef = input.SourceSnapshotRef
		sourceSnapshotDigest = input.SourceSnapshotDigest
	}
	switch input.Status {
	case enum.BuildContextStatusRunning, enum.BuildContextStatusReady:
		if sourceSnapshotRef == "" || !validAgentRunSHA256Digest(sourceSnapshotDigest) {
			return errs.ErrPreconditionFailed
		}
	}
	if input.BuildContextRef != "" && buildContext.BuildContextRef != "" &&
		(buildContext.BuildContextRef != input.BuildContextRef || buildContext.BuildContextDigest != input.BuildContextDigest) {
		return errs.ErrConflict
	}
	return nil
}

func applyBuildContextPendingDiagnostic(buildContext *entity.BuildContext) {
	if buildContext.SourceSnapshotRef == "" {
		buildContext.LastErrorCode = buildContextSourceSnapshotUnavailableCode
		buildContext.LastErrorMessage = buildContextSourceSnapshotUnavailableMessage
		buildContext.NextAction = buildContextSourceSnapshotUnavailableAction
		return
	}
	buildContext.LastErrorCode = buildContextMaterializerUnavailableCode
	buildContext.LastErrorMessage = buildContextMaterializerUnavailableMessage
	buildContext.NextAction = buildContextMaterializerUnavailableAction
}

func validateBuildContextReplayScope(buildContext entity.BuildContext, input PrepareBuildContextInput) error {
	if !sameBuildContextReplayIdentity(buildContext, input) {
		return errs.ErrConflict
	}
	if !sameStringSlices(buildContext.AffectedServiceKeys, input.AffectedServiceKeys) {
		return errs.ErrConflict
	}
	return nil
}

func sameBuildContextReplayIdentity(buildContext entity.BuildContext, input PrepareBuildContextInput) bool {
	actual := []string{
		buildContext.Provider,
		buildContext.ProviderOwner,
		buildContext.ProviderName,
		buildContext.SourceRef,
		buildContext.SourceCommitSHA,
		buildContext.BuildPlanFingerprint,
		buildContext.ContextFingerprint,
	}
	expected := []string{
		input.Provider,
		input.ProviderOwner,
		input.ProviderName,
		input.SourceRef,
		input.SourceCommitSHA,
		input.BuildPlanFingerprint,
		buildContextFingerprint(input),
	}
	return buildContext.ProjectID == input.ProjectID &&
		buildContext.RepositoryID == input.RepositoryID &&
		sameStringSlices(actual, expected)
}

func buildContextFingerprint(input PrepareBuildContextInput) string {
	type fingerprintPayload struct {
		ProjectID            string   `json:"project_id"`
		RepositoryID         string   `json:"repository_id"`
		Provider             string   `json:"provider"`
		ProviderOwner        string   `json:"provider_owner"`
		ProviderName         string   `json:"provider_name"`
		SourceRef            string   `json:"source_ref"`
		SourceCommitSHA      string   `json:"source_commit_sha"`
		AffectedServiceKeys  []string `json:"affected_service_keys"`
		BuildPlanFingerprint string   `json:"build_plan_fingerprint"`
	}
	raw, _ := json.Marshal(fingerprintPayload{
		ProjectID:            input.ProjectID.String(),
		RepositoryID:         input.RepositoryID.String(),
		Provider:             input.Provider,
		ProviderOwner:        input.ProviderOwner,
		ProviderName:         input.ProviderName,
		SourceRef:            input.SourceRef,
		SourceCommitSHA:      input.SourceCommitSHA,
		AffectedServiceKeys:  append([]string(nil), input.AffectedServiceKeys...),
		BuildPlanFingerprint: input.BuildPlanFingerprint,
	})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validBuildContextStatus(status enum.BuildContextStatus) bool {
	switch status {
	case enum.BuildContextStatusPending, enum.BuildContextStatusRunning, enum.BuildContextStatusReady, enum.BuildContextStatusFailed:
		return true
	default:
		return false
	}
}

func terminalBuildContextStatus(status enum.BuildContextStatus) bool {
	switch status {
	case enum.BuildContextStatusReady, enum.BuildContextStatusFailed:
		return true
	default:
		return false
	}
}

func sameStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
