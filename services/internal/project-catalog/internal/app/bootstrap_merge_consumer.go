package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
)

const bootstrapMergeMissingCheckedArtifactCode = "missing_checked_artifact"

type bootstrapMergeDiagnosticRecorder interface {
	RecordBootstrapMergeSignalDiagnostic(context.Context, projectservice.BootstrapMergeSignalDiagnosticInput) error
}

func startProviderBootstrapMergeConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	recorder bootstrapMergeDiagnosticRecorder,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	if !cfg.ProviderBootstrapMergeConsumerEnabled {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	if recorder == nil {
		return fmt.Errorf("project-catalog bootstrap merge consumer requires project service recorder")
	}
	if eventLogPool == nil {
		return fmt.Errorf("project-catalog bootstrap merge consumer requires platform event-log database")
	}
	registry, err := eventconsumer.NewRegistry(eventconsumer.Registration{
		EventType:     providerevents.EventRepositoryBootstrapMerged,
		SchemaVersion: providerevents.SchemaVersion,
		Handler:       bootstrapMergeEventHandler{recorder: recorder},
	})
	if err != nil {
		return err
	}
	runner, err := eventconsumer.NewRunner(eventlog.NewStore(eventLogPool), registry, cfg.ProviderBootstrapMergeConsumerConfig(), logger, nil)
	if err != nil {
		return err
	}
	go func() {
		logger.Info("project-catalog bootstrap merge consumer starting")
		if err := runner.Run(ctx); err != nil {
			errCh <- err
		}
	}()
	return nil
}

type bootstrapMergeEventHandler struct {
	recorder bootstrapMergeDiagnosticRecorder
}

func (h bootstrapMergeEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.AggregateType) != providerevents.AggregateRepositoryMergeSignal {
		return eventconsumer.Poison("invalid_aggregate_type", "bootstrap merge event aggregate type is not repository_merge_signal")
	}
	var payload providerevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", "bootstrap merge event payload is not valid provider payload json")
	}
	projectID, err := parseRequiredUUID(payload.ProjectID)
	if err != nil {
		return eventconsumer.Poison("invalid_project_ref", "bootstrap merge event project_id is invalid")
	}
	repositoryID, err := parseRequiredUUID(payload.RepositoryID)
	if err != nil {
		return eventconsumer.Poison("invalid_repository_ref", "bootstrap merge event repository_id is invalid")
	}
	if strings.TrimSpace(payload.SignalKind) != "bootstrap" {
		return eventconsumer.Poison("invalid_signal_kind", "bootstrap merge event signal_kind is not bootstrap")
	}
	input := projectservice.BootstrapMergeSignalDiagnosticInput{
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		MergeSignal: projectservice.BootstrapRepositoryMergeSignal{
			SignalID:   strings.TrimSpace(payload.RepositoryMergeSignalID),
			SignalKey:  strings.TrimSpace(payload.SignalKey),
			SignalKind: strings.TrimSpace(payload.SignalKind),
			ProviderTarget: projectservice.RepositoryBootstrapProviderTarget{
				ProviderSlug:         strings.TrimSpace(payload.ProviderSlug),
				RepositoryFullName:   strings.TrimSpace(payload.RepositoryFullName),
				ProviderRepositoryID: strings.TrimSpace(payload.ProviderRepositoryID),
			},
			BaseBranch:                   strings.TrimSpace(payload.BaseBranch),
			SourceRef:                    firstNonEmptyString(payload.SourceRef, payload.HeadBranch),
			MergeCommitSHA:               strings.TrimSpace(payload.MergeCommitSHA),
			WatermarkDigest:              strings.TrimSpace(payload.WatermarkDigest),
			ProviderWorkItemProjectionID: strings.TrimSpace(payload.WorkItemProjectionID),
			ProviderWebURL:               strings.TrimSpace(payload.PullRequestURL),
			ProviderObjectID:             strings.TrimSpace(payload.PullRequestProviderID),
			MergeObservedAt:              strings.TrimSpace(payload.ObservedAt),
			MergedAt:                     strings.TrimSpace(payload.MergedAt),
		},
		SignalFingerprint: bootstrapMergeEventFingerprint(storedEvent, payload),
		ErrorCode:         bootstrapMergeMissingCheckedArtifactCode,
		ErrorSummary:      "provider bootstrap merge event does not include checked services policy artifact input",
		Summary:           "provider bootstrap merge signal received",
	}
	if err := h.recorder.RecordBootstrapMergeSignalDiagnostic(ctx, input); err != nil {
		return bootstrapMergeConsumerError(err)
	}
	return eventconsumer.Ack()
}

func bootstrapMergeConsumerError(err error) eventconsumer.Result {
	switch {
	case errors.Is(err, errs.ErrInvalidArgument):
		return eventconsumer.Poison("invalid_signal", "bootstrap merge signal metadata is invalid")
	case errors.Is(err, errs.ErrConflict):
		return eventconsumer.Poison("conflicting_signal", "bootstrap merge signal fingerprint conflicts with stored project state")
	case errors.Is(err, errs.ErrNotFound):
		return eventconsumer.Poison("unknown_binding", "bootstrap merge signal references an unknown project repository binding")
	case errors.Is(err, errs.ErrPreconditionFailed):
		return eventconsumer.Poison("stale_signal", "bootstrap merge signal does not match current project repository state")
	default:
		return eventconsumer.Retry(err)
	}
}

func bootstrapMergeEventFingerprint(_ eventlog.StoredEvent, payload providerevents.Payload) string {
	fingerprintPayload, err := json.Marshal(bootstrapMergeEventFingerprintPayload{
		RepositoryMergeSignalID:  strings.TrimSpace(payload.RepositoryMergeSignalID),
		SignalKey:                strings.TrimSpace(payload.SignalKey),
		SignalKind:               strings.TrimSpace(payload.SignalKind),
		ProjectID:                strings.TrimSpace(payload.ProjectID),
		RepositoryID:             strings.TrimSpace(payload.RepositoryID),
		ProviderSlug:             strings.TrimSpace(payload.ProviderSlug),
		RepositoryFullName:       strings.TrimSpace(payload.RepositoryFullName),
		ProviderRepositoryID:     strings.TrimSpace(payload.ProviderRepositoryID),
		BaseBranch:               strings.TrimSpace(payload.BaseBranch),
		SourceRef:                firstNonEmptyString(payload.SourceRef, payload.HeadBranch),
		MergeCommitSHA:           strings.TrimSpace(payload.MergeCommitSHA),
		WatermarkDigest:          strings.TrimSpace(payload.WatermarkDigest),
		WorkItemProjectionID:     strings.TrimSpace(payload.WorkItemProjectionID),
		PullRequestProviderID:    strings.TrimSpace(payload.PullRequestProviderID),
		RelatedProviderOperation: strings.TrimSpace(payload.RelatedProviderOperationRef),
		Version:                  payload.Version,
	})
	if err != nil {
		fingerprintPayload = []byte(strings.Join([]string{
			strings.TrimSpace(payload.SignalKey),
			strings.TrimSpace(payload.ProjectID),
			strings.TrimSpace(payload.RepositoryID),
			strings.TrimSpace(payload.MergeCommitSHA),
		}, "|"))
	}
	sum := sha256.Sum256(fingerprintPayload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type bootstrapMergeEventFingerprintPayload struct {
	RepositoryMergeSignalID  string `json:"repository_merge_signal_id,omitempty"`
	SignalKey                string `json:"signal_key"`
	SignalKind               string `json:"signal_kind"`
	ProjectID                string `json:"project_id"`
	RepositoryID             string `json:"repository_id"`
	ProviderSlug             string `json:"provider_slug"`
	RepositoryFullName       string `json:"repository_full_name"`
	ProviderRepositoryID     string `json:"provider_repository_id,omitempty"`
	BaseBranch               string `json:"base_branch"`
	SourceRef                string `json:"source_ref"`
	MergeCommitSHA           string `json:"merge_commit_sha"`
	WatermarkDigest          string `json:"watermark_digest,omitempty"`
	WorkItemProjectionID     string `json:"work_item_projection_id,omitempty"`
	PullRequestProviderID    string `json:"pull_request_provider_id,omitempty"`
	RelatedProviderOperation string `json:"related_provider_operation_ref,omitempty"`
	Version                  int64  `json:"version,omitempty"`
}

func parseRequiredUUID(text string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(text))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return parsed, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
