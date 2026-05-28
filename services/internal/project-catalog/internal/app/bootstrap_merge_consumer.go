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
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

const (
	bootstrapMergeMissingCheckedArtifactCode    = "missing_checked_artifact"
	bootstrapMergeIncompleteCheckedArtifactCode = "incomplete_checked_artifact"
	adoptionMergeMissingCheckedArtifactCode     = "missing_checked_artifact"
	adoptionMergeIncompleteCheckedArtifactCode  = "incomplete_checked_artifact"
)

type bootstrapMergeReconciler interface {
	RecordBootstrapMergeSignalDiagnostic(context.Context, projectservice.BootstrapMergeSignalDiagnosticInput) error
	ReconcileBootstrapMergeSignal(context.Context, projectservice.ReconcileBootstrapMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error)
	RecordAdoptionMergeSignalDiagnostic(context.Context, projectservice.AdoptionMergeSignalDiagnosticInput) error
	ReconcileAdoptionMergeSignal(context.Context, projectservice.ReconcileAdoptionMergeSignalInput) (projectservice.BootstrapServicesPolicyImportResult, error)
}

func startProviderBootstrapMergeConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	reconciler bootstrapMergeReconciler,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	return startProviderRepositoryMergeConsumer(ctx, cfg, eventLogPool, reconciler, logger, errCh, "bootstrap")
}

func startProviderAdoptionMergeConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	reconciler bootstrapMergeReconciler,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	return startProviderRepositoryMergeConsumer(ctx, cfg, eventLogPool, reconciler, logger, errCh, "adoption")
}

type providerRepositoryMergeConsumerRuntime struct {
	Enabled   bool
	Label     string
	EventType string
	Handler   eventconsumer.Handler
	Config    eventconsumer.Config
}

func startProviderRepositoryMergeConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	reconciler bootstrapMergeReconciler,
	logger *slog.Logger,
	errCh chan<- error,
	kind string,
) error {
	runtime := providerRepositoryMergeConsumerRuntimeForKind(cfg, reconciler, kind)
	if !runtime.Enabled {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	if reconciler == nil {
		return fmt.Errorf("project-catalog %s merge consumer requires project service reconciler", runtime.Label)
	}
	if eventLogPool == nil {
		return fmt.Errorf("project-catalog %s merge consumer requires platform event-log database", runtime.Label)
	}
	registry, err := eventconsumer.NewRegistry(eventconsumer.Registration{
		EventType:     runtime.EventType,
		SchemaVersion: providerevents.SchemaVersion,
		Handler:       runtime.Handler,
	})
	if err != nil {
		return err
	}
	runner, err := eventconsumer.NewRunner(eventlog.NewStore(eventLogPool), registry, runtime.Config, logger, nil)
	if err != nil {
		return err
	}
	go func() {
		logger.Info("project-catalog " + runtime.Label + " merge consumer starting")
		if err := runner.Run(ctx); err != nil {
			errCh <- err
		}
	}()
	return nil
}

func providerRepositoryMergeConsumerRuntimeForKind(cfg Config, reconciler bootstrapMergeReconciler, kind string) providerRepositoryMergeConsumerRuntime {
	if kind == "adoption" {
		return providerRepositoryMergeConsumerRuntime{
			Enabled:   cfg.ProviderAdoptionMergeConsumerEnabled,
			Label:     "adoption",
			EventType: providerevents.EventRepositoryAdoptionMerged,
			Handler:   adoptionMergeEventHandler{reconciler: reconciler},
			Config:    cfg.ProviderAdoptionMergeConsumerConfig(),
		}
	}
	return providerRepositoryMergeConsumerRuntime{
		Enabled:   cfg.ProviderBootstrapMergeConsumerEnabled,
		Label:     "bootstrap",
		EventType: providerevents.EventRepositoryBootstrapMerged,
		Handler:   bootstrapMergeEventHandler{reconciler: reconciler},
		Config:    cfg.ProviderBootstrapMergeConsumerConfig(),
	}
}

type bootstrapMergeEventHandler struct {
	reconciler bootstrapMergeReconciler
}

func (h bootstrapMergeEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	return repositoryMergeEventHandlerForKind(h.reconciler, "bootstrap").HandleEvent(ctx, event)
}

type adoptionMergeEventHandler struct {
	reconciler bootstrapMergeReconciler
}

func (h adoptionMergeEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	return repositoryMergeEventHandlerForKind(h.reconciler, "adoption").HandleEvent(ctx, event)
}

type repositoryMergeEventHandler struct {
	reconciler               bootstrapMergeReconciler
	expectedSignalKind       string
	signalLabel              string
	missingCheckedCode       string
	incompleteCheckedCode    string
	missingCheckedSummary    string
	incompleteCheckedSummary string
}

func repositoryMergeEventHandlerForKind(reconciler bootstrapMergeReconciler, signalKind string) repositoryMergeEventHandler {
	missingCode := bootstrapMergeMissingCheckedArtifactCode
	incompleteCode := bootstrapMergeIncompleteCheckedArtifactCode
	if signalKind == "adoption" {
		missingCode = adoptionMergeMissingCheckedArtifactCode
		incompleteCode = adoptionMergeIncompleteCheckedArtifactCode
	}
	return repositoryMergeEventHandler{
		reconciler:               reconciler,
		expectedSignalKind:       signalKind,
		signalLabel:              signalKind,
		missingCheckedCode:       missingCode,
		incompleteCheckedCode:    incompleteCode,
		missingCheckedSummary:    "provider " + signalKind + " merge event does not include checked services policy artifact input",
		incompleteCheckedSummary: "provider " + signalKind + " merge event includes incomplete checked services policy artifact input",
	}
}

func (h repositoryMergeEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.AggregateType) != providerevents.AggregateRepositoryMergeSignal {
		return eventconsumer.Poison("invalid_aggregate_type", h.signalLabel+" merge event aggregate type is not repository_merge_signal")
	}
	var payload providerevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", h.signalLabel+" merge event payload is not valid provider payload json")
	}
	projectID, err := parseRequiredUUID(payload.ProjectID)
	if err != nil {
		return eventconsumer.Poison("invalid_project_ref", h.signalLabel+" merge event project_id is invalid")
	}
	repositoryID, err := parseRequiredUUID(payload.RepositoryID)
	if err != nil {
		return eventconsumer.Poison("invalid_repository_ref", h.signalLabel+" merge event repository_id is invalid")
	}
	if strings.TrimSpace(payload.SignalKind) != h.expectedSignalKind {
		return eventconsumer.Poison("invalid_signal_kind", h.signalLabel+" merge event signal_kind is invalid")
	}
	mergeSignal := bootstrapMergeSignalFromPayload(payload)
	if bootstrapMergePayloadHasCompleteCheckedPolicy(payload) {
		err := h.reconcileCheckedPolicy(ctx, storedEvent, projectID, repositoryID, mergeSignal, payload)
		if err != nil {
			return bootstrapMergeConsumerError(err)
		}
		return eventconsumer.Ack()
	}
	diagnosticCode := h.missingCheckedCode
	diagnosticSummary := h.missingCheckedSummary
	if bootstrapMergePayloadHasAnyCheckedPolicy(payload) {
		diagnosticCode = h.incompleteCheckedCode
		diagnosticSummary = h.incompleteCheckedSummary
	}
	if err := h.recordDiagnostic(ctx, projectID, repositoryID, mergeSignal, bootstrapMergeEventFingerprint(storedEvent, payload), diagnosticCode, diagnosticSummary); err != nil {
		return bootstrapMergeConsumerError(err)
	}
	return eventconsumer.Ack()
}

func (h repositoryMergeEventHandler) reconcileCheckedPolicy(ctx context.Context, storedEvent eventlog.StoredEvent, projectID uuid.UUID, repositoryID uuid.UUID, mergeSignal projectservice.BootstrapRepositoryMergeSignal, payload providerevents.Payload) error {
	checkedPolicy := projectservice.CheckedBootstrapServicesPolicyArtifact{
		ArtifactRef:      strings.TrimSpace(payload.CheckedArtifactRef),
		ArtifactDigest:   strings.TrimSpace(payload.CheckedArtifactDigest),
		ArtifactVersion:  strings.TrimSpace(payload.CheckedArtifactVersion),
		SourcePath:       strings.TrimSpace(payload.CheckedSourcePath),
		ContentHash:      strings.TrimSpace(payload.CheckedContentHash),
		ValidatedPayload: []byte(strings.TrimSpace(payload.CheckedValidatedPayloadJSON)),
	}
	if h.expectedSignalKind == "adoption" {
		_, err := h.reconciler.ReconcileAdoptionMergeSignal(ctx, projectservice.ReconcileAdoptionMergeSignalInput{
			ProjectID:     projectID,
			RepositoryID:  repositoryID,
			MergeSignal:   mergeSignal,
			CheckedPolicy: checkedPolicy,
			Meta:          repositoryMergeEventCommandMeta(storedEvent, payload),
		})
		return err
	}
	_, err := h.reconciler.ReconcileBootstrapMergeSignal(ctx, projectservice.ReconcileBootstrapMergeSignalInput{
		ProjectID:     projectID,
		RepositoryID:  repositoryID,
		MergeSignal:   mergeSignal,
		CheckedPolicy: checkedPolicy,
		Meta:          repositoryMergeEventCommandMeta(storedEvent, payload),
	})
	return err
}

func repositoryMergeEventCommandMeta(storedEvent eventlog.StoredEvent, payload providerevents.Payload) value.CommandMeta {
	return value.CommandMeta{
		Actor: value.Actor{
			Type: "service",
			ID:   "provider-hub",
		},
		RequestID: strings.TrimSpace(storedEvent.Event.ID.String()),
		RequestContext: value.RequestContext{
			Source:  "platform-event-log",
			TraceID: strings.TrimSpace(payload.RelatedProviderOperationRef),
		},
		OccurredAt: storedEvent.Event.OccurredAt,
	}
}

func (h repositoryMergeEventHandler) recordDiagnostic(ctx context.Context, projectID uuid.UUID, repositoryID uuid.UUID, mergeSignal projectservice.BootstrapRepositoryMergeSignal, fingerprint string, code string, summary string) error {
	if h.expectedSignalKind == "adoption" {
		return h.reconciler.RecordAdoptionMergeSignalDiagnostic(ctx, projectservice.AdoptionMergeSignalDiagnosticInput{
			ProjectID:         projectID,
			RepositoryID:      repositoryID,
			MergeSignal:       mergeSignal,
			SignalFingerprint: fingerprint,
			ErrorCode:         code,
			ErrorSummary:      summary,
			Summary:           "provider adoption merge signal received",
		})
	}
	return h.reconciler.RecordBootstrapMergeSignalDiagnostic(ctx, projectservice.BootstrapMergeSignalDiagnosticInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		MergeSignal:       mergeSignal,
		SignalFingerprint: fingerprint,
		ErrorCode:         code,
		ErrorSummary:      summary,
		Summary:           "provider bootstrap merge signal received",
	})
}

func bootstrapMergeSignalFromPayload(payload providerevents.Payload) projectservice.BootstrapRepositoryMergeSignal {
	return projectservice.BootstrapRepositoryMergeSignal{
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
		WatermarkJSON:                []byte(strings.TrimSpace(payload.CheckedWatermarkJSON)),
		ProviderWorkItemProjectionID: strings.TrimSpace(payload.WorkItemProjectionID),
		ProviderWebURL:               strings.TrimSpace(payload.PullRequestURL),
		ProviderObjectID:             strings.TrimSpace(payload.PullRequestProviderID),
		MergeObservedAt:              strings.TrimSpace(payload.ObservedAt),
		MergedAt:                     strings.TrimSpace(payload.MergedAt),
	}
}

func bootstrapMergePayloadHasCompleteCheckedPolicy(payload providerevents.Payload) bool {
	for _, field := range bootstrapMergeCheckedPolicyFields(payload) {
		if field == "" {
			return false
		}
	}
	return true
}

func bootstrapMergePayloadHasAnyCheckedPolicy(payload providerevents.Payload) bool {
	for _, field := range bootstrapMergeCheckedPolicyFields(payload) {
		if field != "" {
			return true
		}
	}
	return false
}

func bootstrapMergeCheckedPolicyFields(payload providerevents.Payload) []string {
	return []string{
		strings.TrimSpace(payload.CheckedArtifactRef),
		strings.TrimSpace(payload.CheckedArtifactDigest),
		strings.TrimSpace(payload.CheckedArtifactVersion),
		strings.TrimSpace(payload.CheckedSourcePath),
		strings.TrimSpace(payload.CheckedContentHash),
		strings.TrimSpace(payload.CheckedValidatedPayloadJSON),
		strings.TrimSpace(payload.CheckedWatermarkJSON),
	}
}

func bootstrapMergeConsumerError(err error) eventconsumer.Result {
	switch {
	case errors.Is(err, errs.ErrInvalidArgument):
		return eventconsumer.Poison("invalid_signal", "repository merge signal metadata is invalid")
	case errors.Is(err, errs.ErrConflict):
		return eventconsumer.Poison("conflicting_signal", "repository merge signal fingerprint conflicts with stored project state")
	case errors.Is(err, errs.ErrNotFound):
		return eventconsumer.Poison("unknown_binding", "repository merge signal references an unknown project repository binding")
	case errors.Is(err, errs.ErrPreconditionFailed):
		return eventconsumer.Poison("stale_signal", "repository merge signal does not match current project repository state")
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
