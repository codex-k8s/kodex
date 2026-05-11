package service

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

const (
	artifactSignalIDPrefix = "artifact-signal:"
	artifactSignalAccepted = "accepted"
)

// RegisterProviderArtifactSignal turns an internal artifact signal into hot reconciliation cursors.
func (s *Service) RegisterProviderArtifactSignal(ctx context.Context, input RegisterProviderArtifactSignalInput) (ProviderArtifactSignalResult, error) {
	target := normalizedProviderArtifactTarget(input.Target)
	source := strings.TrimSpace(input.Source)
	if !validProviderSlug(target.ProviderSlug) ||
		input.ExternalAccountID == uuid.Nil ||
		source == "" ||
		input.ObservedAt.IsZero() {
		return ProviderArtifactSignalResult{}, errs.ErrInvalidArgument
	}
	if len(bytes.TrimSpace(input.PayloadJSON)) > 0 {
		if _, err := canonicalJSONObject(input.PayloadJSON); err != nil {
			return ProviderArtifactSignalResult{}, err
		}
	}
	scopeType, scopeRef, artifactKinds, err := artifactSignalScope(target)
	if err != nil {
		return ProviderArtifactSignalResult{}, err
	}
	sort.Slice(artifactKinds, func(i, j int) bool {
		return artifactKinds[i] < artifactKinds[j]
	})
	signalID, idempotencyKey, err := artifactSignalIdentity(input.SignalID, input.Meta, source, target.ProviderSlug, scopeType, scopeRef, input.ObservedAt)
	if err != nil {
		return ProviderArtifactSignalResult{}, err
	}

	now := s.clock.Now().UTC()
	request := entity.ReconciliationRequest{
		ID:                s.ids.New(),
		ProviderSlug:      target.ProviderSlug,
		ExternalAccountID: input.ExternalAccountID,
		ScopeType:         scopeType,
		ScopeRef:          scopeRef,
		IdempotencyKey:    idempotencyKey,
		ArtifactKinds:     artifactKinds,
		Priority:          enum.SyncCursorPriorityHot,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	cursors := s.buildSyncCursors(target.ProviderSlug, input.ExternalAccountID, scopeType, scopeRef, artifactKinds, enum.SyncCursorPriorityHot, now)
	if _, err := s.repository.EnqueueSyncCursors(ctx, request, cursors); err != nil {
		return ProviderArtifactSignalResult{}, err
	}
	return ProviderArtifactSignalResult{SignalID: signalID, Status: artifactSignalAccepted, Target: target}, nil
}

func normalizedProviderArtifactTarget(target ProviderArtifactTarget) ProviderArtifactTarget {
	return ProviderArtifactTarget{
		ProviderSlug:         enum.ProviderSlug(strings.TrimSpace(string(target.ProviderSlug))),
		RepositoryFullName:   strings.TrimSpace(target.RepositoryFullName),
		ProviderRepositoryID: strings.TrimSpace(target.ProviderRepositoryID),
		WorkItemKind:         enum.WorkItemKind(strings.TrimSpace(string(target.WorkItemKind))),
		Number:               target.Number,
		ProviderObjectID:     strings.TrimSpace(target.ProviderObjectID),
		WebURL:               strings.TrimSpace(target.WebURL),
	}
}

func artifactSignalScope(target ProviderArtifactTarget) (enum.SyncCursorScopeType, string, []enum.SyncArtifactKind, error) {
	if target.Number < 0 {
		return "", "", nil, errs.ErrInvalidArgument
	}
	if target.WorkItemKind != "" {
		if !validWorkItemKind(target.WorkItemKind) {
			return "", "", nil, errs.ErrInvalidArgument
		}
		scopeRef := artifactSignalWorkItemScopeRef(target)
		if scopeRef == "" {
			return "", "", nil, errs.ErrInvalidArgument
		}
		return enum.SyncCursorScopeWorkItem, scopeRef, artifactKindsForWorkItemSignal(target.WorkItemKind), nil
	}
	if target.RepositoryFullName != "" {
		return enum.SyncCursorScopeRepository, target.RepositoryFullName, []enum.SyncArtifactKind{enum.SyncArtifactRepository}, nil
	}
	if target.ProviderRepositoryID != "" {
		return enum.SyncCursorScopeRepository, "provider_repository_id:" + target.ProviderRepositoryID, []enum.SyncArtifactKind{enum.SyncArtifactRepository}, nil
	}
	return "", "", nil, errs.ErrInvalidArgument
}

func artifactSignalWorkItemScopeRef(target ProviderArtifactTarget) string {
	if target.RepositoryFullName != "" && target.Number > 0 {
		return fmt.Sprintf("%s#%s:%s", target.RepositoryFullName, target.WorkItemKind, strconv.FormatInt(target.Number, 10))
	}
	if target.ProviderObjectID != "" {
		return "provider_object_id:" + target.ProviderObjectID
	}
	if target.WebURL != "" {
		return "web_url:" + target.WebURL
	}
	return ""
}

func artifactKindsForWorkItemSignal(kind enum.WorkItemKind) []enum.SyncArtifactKind {
	switch kind {
	case enum.WorkItemKindIssue:
		return []enum.SyncArtifactKind{enum.SyncArtifactIssue, enum.SyncArtifactComment, enum.SyncArtifactRelationship}
	case enum.WorkItemKindPullRequest:
		return []enum.SyncArtifactKind{enum.SyncArtifactPullRequest, enum.SyncArtifactComment, enum.SyncArtifactRelationship}
	case enum.WorkItemKindMergeRequest:
		return []enum.SyncArtifactKind{enum.SyncArtifactMergeRequest, enum.SyncArtifactComment, enum.SyncArtifactRelationship}
	default:
		return nil
	}
}

func artifactSignalIdentity(signalID string, meta value.CommandMeta, source string, providerSlug enum.ProviderSlug, scopeType enum.SyncCursorScopeType, scopeRef string, observedAt time.Time) (string, string, error) {
	cleanSignalID := strings.TrimSpace(signalID)
	identity := cleanSignalID
	switch {
	case identity != "":
	case strings.TrimSpace(meta.IdempotencyKey) != "":
		identity = strings.TrimSpace(meta.IdempotencyKey)
	case meta.CommandID != uuid.Nil:
		identity = meta.CommandID.String()
	case !observedAt.IsZero():
		window := observedAt.UTC().Truncate(time.Minute).Format(time.RFC3339)
		identity = strings.Join([]string{source, string(providerSlug), string(scopeType), scopeRef, window}, "|")
	}
	if identity == "" {
		return "", "", errs.ErrInvalidArgument
	}
	if cleanSignalID == "" {
		cleanSignalID = identity
	}
	return cleanSignalID, artifactSignalIDPrefix + identity, nil
}

func (s *Service) buildSyncCursors(providerSlug enum.ProviderSlug, externalAccountID uuid.UUID, scopeType enum.SyncCursorScopeType, scopeRef string, artifactKinds []enum.SyncArtifactKind, priority enum.SyncCursorPriority, now time.Time) []entity.SyncCursor {
	cursors := make([]entity.SyncCursor, 0, len(artifactKinds))
	for _, artifactKind := range artifactKinds {
		cursors = append(cursors, entity.SyncCursor{
			Base: entity.Base{
				ID:        s.ids.New(),
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
			},
			ProviderSlug:        providerSlug,
			ExternalAccountID:   externalAccountID,
			ScopeType:           scopeType,
			ScopeRef:            scopeRef,
			ArtifactKind:        artifactKind,
			Priority:            priority,
			RateBudgetStateJSON: []byte(`{}`),
		})
	}
	return cursors
}
