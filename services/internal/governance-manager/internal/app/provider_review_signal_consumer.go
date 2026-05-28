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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	providerReviewSignalSourceService = "provider-hub"
	providerReviewSignalActor         = "provider-hub"
	providerReviewSignalTargetType    = "provider_work_item"
	providerReviewSignalEvidenceKind  = "provider_review_signal"
	providerReviewSignalRetention     = "safe_ref"
)

type reviewSignalRecorder interface {
	RecordReviewSignal(context.Context, governanceservice.RecordReviewSignalInput) (entity.ReviewSignal, error)
}

func startProviderReviewSignalConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	recorder reviewSignalRecorder,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	if !cfg.ProviderReviewSignalConsumerEnabled {
		return nil
	}
	logger = providerReviewSignalLogger(logger)
	runner, err := newProviderReviewSignalRunner(cfg, eventLogPool, recorder, logger)
	if err != nil {
		return err
	}
	go runProviderReviewSignalConsumer(ctx, runner, logger, errCh)
	return nil
}

func providerReviewSignalLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

func validateProviderReviewSignalConsumer(eventLogPool *pgxpool.Pool, recorder reviewSignalRecorder) error {
	switch {
	case recorder == nil:
		return fmt.Errorf("governance-manager provider review signal consumer requires review signal recorder")
	case eventLogPool == nil:
		return fmt.Errorf("governance-manager provider review signal consumer requires platform event-log database")
	default:
		return nil
	}
}

func newProviderReviewSignalRunner(cfg Config, eventLogPool *pgxpool.Pool, recorder reviewSignalRecorder, logger *slog.Logger) (*eventconsumer.Runner, error) {
	if err := validateProviderReviewSignalConsumer(eventLogPool, recorder); err != nil {
		return nil, err
	}
	registry, err := providerReviewSignalRegistry(recorder)
	if err != nil {
		return nil, fmt.Errorf("build provider review signal consumer registry: %w", err)
	}
	store := eventlog.NewStore(eventLogPool)
	runtime := cfg.ProviderReviewSignalConsumerConfig()
	runner, err := eventconsumer.NewRunner(store, registry, runtime, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("build provider review signal consumer runner: %w", err)
	}
	return runner, nil
}

func providerReviewSignalRegistry(recorder reviewSignalRecorder) (eventconsumer.Registry, error) {
	return eventconsumer.NewRegistry(eventconsumer.Registration{
		EventType:     providerevents.EventCommentSynced,
		SchemaVersion: providerevents.SchemaVersion,
		Handler:       providerReviewSignalEventHandler{recorder: recorder},
	})
}

func runProviderReviewSignalConsumer(ctx context.Context, runner *eventconsumer.Runner, logger *slog.Logger, errCh chan<- error) {
	logger.Info("governance-manager provider review signal consumer starting")
	if err := runner.Run(ctx); err != nil {
		errCh <- err
	}
}

type providerReviewSignalEventHandler struct {
	recorder reviewSignalRecorder
}

func (h providerReviewSignalEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.SourceService) != providerReviewSignalSourceService {
		return eventconsumer.Poison("invalid_source_service", "provider review signal event source service is not provider-hub")
	}
	if strings.TrimSpace(storedEvent.AggregateType) != providerevents.AggregateComment {
		return eventconsumer.Poison("invalid_aggregate_type", "provider review signal event aggregate type is not comment")
	}
	var payload providerevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", "provider review signal event payload is not valid provider json")
	}
	input, result := providerReviewSignalInput(storedEvent, payload)
	if result.Status != "" {
		return result
	}
	if _, err := h.recorder.RecordReviewSignal(ctx, input); err != nil {
		return providerReviewSignalConsumerError(err)
	}
	return eventconsumer.Ack()
}

func providerReviewSignalInput(storedEvent eventlog.StoredEvent, payload providerevents.Payload) (governanceservice.RecordReviewSignalInput, eventconsumer.Result) {
	outcome, severity, summary, ok := providerReviewSignalOutcome(payload.ReviewState)
	if !ok {
		return governanceservice.RecordReviewSignalInput{}, eventconsumer.Ack()
	}
	targetRef := strings.TrimSpace(payload.ProviderWorkItemID)
	if targetRef == "" {
		return governanceservice.RecordReviewSignalInput{}, eventconsumer.Poison("missing_provider_work_item_ref", "provider review signal event misses provider work item ref")
	}
	evidenceRef := providerReviewSignalEvidenceRef(payload)
	if evidenceRef.Ref == "" {
		return governanceservice.RecordReviewSignalInput{}, eventconsumer.Poison("missing_provider_review_ref", "provider review signal event misses provider review ref")
	}
	return governanceservice.RecordReviewSignalInput{
		Target: value.ExternalRef{
			Type: providerReviewSignalTargetType,
			Ref:  targetRef,
		},
		RoleKind:   enum.ReviewRoleKindReviewer,
		AuthorRef:  "service:" + providerReviewSignalActor,
		Outcome:    outcome,
		Severity:   severity,
		Confidence: enum.ConfidenceHigh,
		EvidenceRefs: []value.EvidenceRef{
			evidenceRef,
		},
		Summary: summary,
		Meta: governanceservice.CommandMeta{
			IdempotencyKey: providerReviewSignalIdempotencyKey(targetRef, evidenceRef.Ref, string(outcome)),
			Actor:          value.Actor{Type: "service", ID: providerReviewSignalActor},
			RequestID:      providerReviewSignalRequestID(storedEvent),
		},
	}, eventconsumer.Result{}
}

func providerReviewSignalOutcome(reviewState string) (enum.ReviewSignalOutcome, enum.SignalSeverity, string, bool) {
	switch strings.ToLower(strings.TrimSpace(reviewState)) {
	case "approved":
		return enum.ReviewSignalOutcomePass, enum.SignalSeverityInfo, "provider review approved", true
	case "changes_requested":
		return enum.ReviewSignalOutcomeRequestChanges, enum.SignalSeverityBlocking, "provider review requested changes", true
	default:
		return "", "", "", false
	}
}

func providerReviewSignalEvidenceRef(payload providerevents.Payload) value.EvidenceRef {
	if ref := strings.TrimSpace(payload.CommentProjectionID); ref != "" {
		return value.EvidenceRef{
			Kind:           providerReviewSignalEvidenceKind,
			Ref:            "provider:comment_projection/" + ref,
			Summary:        "provider review signal",
			RetentionClass: providerReviewSignalRetention,
		}
	}
	providerSlug := strings.TrimSpace(payload.ProviderSlug)
	commentID := strings.TrimSpace(payload.ProviderCommentID)
	if providerSlug == "" || commentID == "" {
		return value.EvidenceRef{}
	}
	return value.EvidenceRef{
		Kind:           providerReviewSignalEvidenceKind,
		Ref:            "provider:" + providerSlug + ":comment/" + commentID,
		Summary:        "provider review signal",
		RetentionClass: providerReviewSignalRetention,
	}
}

func providerReviewSignalIdempotencyKey(targetRef string, evidenceRef string, outcome string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(targetRef),
		strings.TrimSpace(evidenceRef),
		strings.TrimSpace(outcome),
	}, "\x00")))
	return "provider_review_signal:" + hex.EncodeToString(sum[:])
}

func providerReviewSignalRequestID(storedEvent eventlog.StoredEvent) string {
	if storedEvent.ID.String() == "" {
		return ""
	}
	return "provider_event:" + storedEvent.ID.String()
}

var providerReviewSignalDomainErrorResults = []struct {
	target error
	result eventconsumer.Result
}{
	{
		target: errs.ErrInvalidArgument,
		result: eventconsumer.Poison("invalid_provider_review_signal", "provider review signal metadata is invalid"),
	},
	{
		target: errs.ErrConflict,
		result: eventconsumer.Poison("conflicting_provider_review_signal", "provider review signal conflicts with stored governance evidence"),
	},
	{
		target: errs.ErrForbidden,
		result: eventconsumer.Poison("forbidden_provider_review_signal", "provider review signal actor is not authorized"),
	},
	{
		target: errs.ErrNotFound,
		result: eventconsumer.Poison("unknown_provider_review_signal_ref", "provider review signal references unknown governance state"),
	},
	{
		target: errs.ErrPreconditionFailed,
		result: eventconsumer.Poison("stale_provider_review_signal", "provider review signal cannot update current governance state"),
	},
}

func providerReviewSignalConsumerError(err error) eventconsumer.Result {
	for _, candidate := range providerReviewSignalDomainErrorResults {
		if errors.Is(err, candidate.target) {
			return candidate.result
		}
	}
	return eventconsumer.Retry(err)
}
