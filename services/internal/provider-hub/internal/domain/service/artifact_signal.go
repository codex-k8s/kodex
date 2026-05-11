package service

import (
	"bytes"
	"context"
	"encoding/json"
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
	artifactSignalIDPrefix        = "artifact-signal:"
	artifactSignalAccepted        = "accepted"
	artifactSignalEmptyPayloadRaw = `{}`
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
	observedAt := input.ObservedAt.UTC().Round(time.Microsecond)
	payloadJSON := []byte(artifactSignalEmptyPayloadRaw)
	if len(bytes.TrimSpace(input.PayloadJSON)) > 0 {
		compactPayload, err := canonicalJSONObject(input.PayloadJSON)
		if err != nil {
			return ProviderArtifactSignalResult{}, err
		}
		payloadJSON = compactPayload
	}
	scopeType, scopeRef, artifactKinds, err := artifactSignalScope(target)
	if err != nil {
		return ProviderArtifactSignalResult{}, err
	}
	sort.Slice(artifactKinds, func(i, j int) bool {
		return artifactKinds[i] < artifactKinds[j]
	})
	signalID, idempotencyKey, err := artifactSignalIdentity(input.SignalID, input.Meta, source, target.ProviderSlug, scopeType, scopeRef, observedAt)
	if err != nil {
		return ProviderArtifactSignalResult{}, err
	}
	targetJSON, err := artifactSignalTargetJSON(target)
	if err != nil {
		return ProviderArtifactSignalResult{}, err
	}

	now := s.clock.Now().UTC()
	storedSignal, err := s.repository.StoreProviderArtifactSignal(ctx, entity.ProviderArtifactSignal{
		ID:                s.ids.New(),
		IdentityKey:       idempotencyKey,
		ProviderSlug:      target.ProviderSlug,
		ExternalAccountID: input.ExternalAccountID,
		Source:            source,
		ScopeType:         scopeType,
		ScopeRef:          scopeRef,
		ArtifactKinds:     artifactKinds,
		TargetJSON:        targetJSON,
		PayloadJSON:       payloadJSON,
		ObservedAt:        observedAt,
		CreatedAt:         now,
	})
	if err != nil {
		return ProviderArtifactSignalResult{}, err
	}
	request := entity.ReconciliationRequest{
		ID:                s.ids.New(),
		ProviderSlug:      target.ProviderSlug,
		ExternalAccountID: input.ExternalAccountID,
		ScopeType:         scopeType,
		ScopeRef:          scopeRef,
		IdempotencyKey:    storedSignal.IdentityKey,
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
	if target.WorkItemKind != "" && !validWorkItemKind(target.WorkItemKind) {
		return "", "", nil, errs.ErrInvalidArgument
	}
	if target.Number > 0 && target.RepositoryFullName == "" && target.ProviderObjectID == "" && target.WebURL == "" {
		return "", "", nil, errs.ErrInvalidArgument
	}

	if target.WorkItemKind != "" || target.Number > 0 || target.ProviderObjectID != "" || target.WebURL != "" {
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
		kind := string(target.WorkItemKind)
		if kind == "" {
			kind = "number"
		}
		return fmt.Sprintf("%s#%s:%s", target.RepositoryFullName, kind, strconv.FormatInt(target.Number, 10))
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
	case "":
		return []enum.SyncArtifactKind{
			enum.SyncArtifactIssue,
			enum.SyncArtifactPullRequest,
			enum.SyncArtifactMergeRequest,
			enum.SyncArtifactComment,
			enum.SyncArtifactRelationship,
		}
	default:
		return nil
	}
}

func artifactSignalIdentity(signalID string, meta value.CommandMeta, source string, providerSlug enum.ProviderSlug, scopeType enum.SyncCursorScopeType, scopeRef string, observedAt time.Time) (string, string, error) {
	cleanSignalID := strings.TrimSpace(signalID)
	switch {
	case cleanSignalID != "":
		return cleanSignalID, artifactSignalIDPrefix + "id:" + cleanSignalID, nil
	case strings.TrimSpace(meta.IdempotencyKey) != "":
		key := strings.TrimSpace(meta.IdempotencyKey)
		return key, artifactSignalIDPrefix + "idempotency:" + key, nil
	case meta.CommandID != uuid.Nil:
		key := meta.CommandID.String()
		return key, artifactSignalIDPrefix + "command:" + key, nil
	case !observedAt.IsZero():
		window := observedAt.UTC().Truncate(time.Minute).Format(time.RFC3339)
		key := strings.Join([]string{source, string(providerSlug), string(scopeType), scopeRef, window}, "|")
		return key, artifactSignalIDPrefix + "window:" + key, nil
	default:
		return "", "", errs.ErrInvalidArgument
	}
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

type artifactSignalTargetSnapshot struct {
	ProviderSlug         enum.ProviderSlug `json:"provider_slug"`
	RepositoryFullName   string            `json:"repository_full_name,omitempty"`
	ProviderRepositoryID string            `json:"provider_repository_id,omitempty"`
	WorkItemKind         enum.WorkItemKind `json:"work_item_kind,omitempty"`
	Number               int64             `json:"number,omitempty"`
	ProviderObjectID     string            `json:"provider_object_id,omitempty"`
	WebURL               string            `json:"web_url,omitempty"`
}

func artifactSignalTargetJSON(target ProviderArtifactTarget) ([]byte, error) {
	raw, err := json.Marshal(artifactSignalTargetSnapshot(target))
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return raw, nil
}
