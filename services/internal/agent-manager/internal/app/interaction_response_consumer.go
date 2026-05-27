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
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	interactionResponseSourceService = "interaction-hub"
	interactionResponseConsumerActor = "interaction-hub"
	interactionResponseRequestKind   = "human_gate"
	interactionResponseOwnerService  = "agent_manager"
	interactionResponseAnswered      = "answered"
)

type humanGateResponseRecorder interface {
	GetHumanGateRequest(context.Context, uuid.UUID) (entity.HumanGateRequest, error)
	RecordHumanGateDecision(context.Context, agentservice.RecordHumanGateDecisionInput) (entity.HumanGateRequest, error)
}

func startInteractionResponseConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	recorder humanGateResponseRecorder,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	starter := interactionResponseConsumerStarter{
		cfg:          cfg,
		eventLogPool: eventLogPool,
		recorder:     recorder,
		logger:       logger,
		errCh:        errCh,
	}
	return starter.start(ctx)
}

type interactionResponseConsumerStarter struct {
	cfg          Config
	eventLogPool *pgxpool.Pool
	recorder     humanGateResponseRecorder
	logger       *slog.Logger
	errCh        chan<- error
}

func (s interactionResponseConsumerStarter) start(ctx context.Context) error {
	if !s.cfg.InteractionResponseConsumerEnabled {
		return nil
	}
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := s.validate(); err != nil {
		return err
	}
	runner, err := s.runner(logger)
	if err != nil {
		return err
	}
	go runInteractionResponseConsumer(ctx, runner, logger, s.errCh)
	return nil
}

func (s interactionResponseConsumerStarter) validate() error {
	switch {
	case s.recorder == nil:
		return fmt.Errorf("agent-manager interaction response consumer requires human gate recorder")
	case s.eventLogPool == nil:
		return fmt.Errorf("agent-manager interaction response consumer requires platform event-log database")
	default:
		return nil
	}
}

func (s interactionResponseConsumerStarter) runner(logger *slog.Logger) (*eventconsumer.Runner, error) {
	registry, err := eventconsumer.NewRegistry(interactionResponseConsumerRegistration(s.recorder))
	if err != nil {
		return nil, err
	}
	return eventconsumer.NewRunner(
		eventlog.NewStore(s.eventLogPool),
		registry,
		s.cfg.InteractionResponseConsumerConfig(),
		logger,
		nil,
	)
}

func interactionResponseConsumerRegistration(recorder humanGateResponseRecorder) eventconsumer.Registration {
	return eventconsumer.Registration{
		EventType:     interactionevents.EventRequestResponseRecorded,
		SchemaVersion: interactionevents.SchemaVersion,
		Handler:       interactionResponseEventHandler{recorder: recorder},
	}
}

func runInteractionResponseConsumer(ctx context.Context, runner *eventconsumer.Runner, logger *slog.Logger, errCh chan<- error) {
	logger.Info("agent-manager interaction response consumer starting")
	if err := runner.Run(ctx); err != nil {
		errCh <- err
	}
}

type interactionResponseEventHandler struct {
	recorder humanGateResponseRecorder
}

func (h interactionResponseEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.SourceService) != interactionResponseSourceService {
		return eventconsumer.Poison("invalid_source_service", "interaction response event source service is not interaction-hub")
	}
	if strings.TrimSpace(storedEvent.AggregateType) != interactionevents.AggregateRequest {
		return eventconsumer.Poison("invalid_aggregate_type", "interaction response event aggregate type is not request")
	}
	var payload interactionevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", "interaction response event payload is not valid interaction json")
	}
	if !interactionResponseTargetsAgentHumanGate(payload) {
		return eventconsumer.Ack()
	}
	input, result := h.interactionResponseDecisionInput(ctx, payload)
	if result.Status != "" {
		return result
	}
	if _, err := h.recorder.RecordHumanGateDecision(ctx, input); err != nil {
		return interactionResponseConsumerError(err)
	}
	return eventconsumer.Ack()
}

func (h interactionResponseEventHandler) interactionResponseDecisionInput(ctx context.Context, payload interactionevents.Payload) (agentservice.RecordHumanGateDecisionInput, eventconsumer.Result) {
	if strings.TrimSpace(payload.Status) != interactionResponseAnswered {
		return agentservice.RecordHumanGateDecisionInput{}, eventconsumer.Poison("invalid_response_status", "interaction response event is not answered")
	}
	if strings.TrimSpace(payload.RequestID) == "" || strings.TrimSpace(payload.ResponseID) == "" {
		return agentservice.RecordHumanGateDecisionInput{}, eventconsumer.Poison("missing_response_refs", "interaction response event misses request or response ref")
	}
	if payload.Version < 1 {
		return agentservice.RecordHumanGateDecisionInput{}, eventconsumer.Poison("invalid_response_version", "interaction response event version is invalid")
	}
	gateID, parseErr := parseHumanGateOwnerRequestRef(payload.OwnerRequestRef)
	if parseErr != nil {
		return agentservice.RecordHumanGateDecisionInput{}, eventconsumer.Poison("invalid_owner_request_ref", "interaction response event owner_request_ref is not a human gate ref")
	}
	outcome, outcomeErr := interactionResponseOutcome(payload.ResponseAction)
	if outcomeErr != nil {
		return agentservice.RecordHumanGateDecisionInput{}, eventconsumer.Poison("unsupported_response_action", "interaction response action cannot resolve human gate")
	}
	gate, loadErr := h.recorder.GetHumanGateRequest(ctx, gateID)
	if loadErr != nil {
		return agentservice.RecordHumanGateDecisionInput{}, interactionResponseConsumerError(loadErr)
	}
	expectedVersion := gate.Version
	return agentservice.RecordHumanGateDecisionInput{
		Meta: value.CommandMeta{
			IdempotencyKey:  interactionResponseIdempotencyKey(payload.ResponseID),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: interactionResponseConsumerActor},
		},
		HumanGateRequestID:             gateID,
		Status:                         enum.HumanGateStatusResolved,
		Outcome:                        outcome,
		SafeSummary:                    gate.SafeSummary,
		InteractionRequestRef:          interactionRef("request", payload.RequestID),
		InteractionResponseRef:         interactionRef("response", payload.ResponseID),
		InteractionResponseFingerprint: interactionResponseFingerprint(payload),
		InteractionRequestVersion:      payload.Version,
	}, eventconsumer.Result{}
}

func interactionResponseTargetsAgentHumanGate(payload interactionevents.Payload) bool {
	return strings.TrimSpace(payload.OwnerService) == interactionResponseOwnerService &&
		strings.TrimSpace(payload.RequestKind) == interactionResponseRequestKind
}

func interactionResponseOutcome(action string) (enum.HumanGateOutcome, error) {
	switch strings.TrimSpace(action) {
	case string(enum.HumanGateOutcomeApprove):
		return enum.HumanGateOutcomeApprove, nil
	case string(enum.HumanGateOutcomeReject):
		return enum.HumanGateOutcomeReject, nil
	case string(enum.HumanGateOutcomeRequestChanges):
		return enum.HumanGateOutcomeRequestChanges, nil
	case string(enum.HumanGateOutcomeAnswer):
		return enum.HumanGateOutcomeAnswer, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func interactionResponseIdempotencyKey(responseID string) string {
	return "interaction_response:" + strings.TrimSpace(responseID)
}

func interactionRef(kind string, id string) string {
	id = strings.TrimSpace(id)
	if strings.Contains(id, ":") {
		return id
	}
	return "interaction:" + kind + "/" + id
}

func parseHumanGateOwnerRequestRef(ref string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	candidates := []string{trimmed}
	for _, separator := range []string{"/", ":", "#"} {
		if index := strings.LastIndex(trimmed, separator); index >= 0 && index < len(trimmed)-1 {
			candidates = append(candidates, trimmed[index+1:])
		}
	}
	for _, candidate := range candidates {
		parsed, err := uuid.Parse(strings.TrimSpace(candidate))
		if err == nil && parsed != uuid.Nil {
			return parsed, nil
		}
	}
	return uuid.Nil, errs.ErrInvalidArgument
}

func interactionResponseFingerprint(payload interactionevents.Payload) string {
	fingerprintPayload, err := json.Marshal(interactionResponseFingerprintPayload{
		RequestID:        strings.TrimSpace(payload.RequestID),
		RequestKind:      strings.TrimSpace(payload.RequestKind),
		ResponseID:       strings.TrimSpace(payload.ResponseID),
		ResponseAction:   strings.TrimSpace(payload.ResponseAction),
		OwnerService:     strings.TrimSpace(payload.OwnerService),
		OwnerRequestRef:  strings.TrimSpace(payload.OwnerRequestRef),
		OwnerDecisionRef: strings.TrimSpace(payload.OwnerDecisionRef),
		SourceKind:       strings.TrimSpace(payload.SourceKind),
		Status:           strings.TrimSpace(payload.Status),
		Version:          payload.Version,
	})
	if err != nil {
		fingerprintPayload = []byte(strings.Join([]string{
			strings.TrimSpace(payload.RequestID),
			strings.TrimSpace(payload.ResponseID),
			strings.TrimSpace(payload.ResponseAction),
			strings.TrimSpace(payload.OwnerRequestRef),
		}, "|"))
	}
	sum := sha256.Sum256(fingerprintPayload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type interactionResponseFingerprintPayload struct {
	RequestID        string `json:"request_id"`
	RequestKind      string `json:"request_kind"`
	ResponseID       string `json:"response_id"`
	ResponseAction   string `json:"response_action"`
	OwnerService     string `json:"owner_service"`
	OwnerRequestRef  string `json:"owner_request_ref"`
	OwnerDecisionRef string `json:"owner_decision_ref,omitempty"`
	SourceKind       string `json:"source_kind,omitempty"`
	Status           string `json:"status"`
	Version          int64  `json:"version"`
}

var interactionResponseDomainErrorResults = []struct {
	target error
	result eventconsumer.Result
}{
	{
		target: errs.ErrInvalidArgument,
		result: eventconsumer.Poison("invalid_human_gate_response", "interaction response metadata is invalid"),
	},
	{
		target: errs.ErrConflict,
		result: eventconsumer.Poison("conflicting_human_gate_response", "interaction response conflicts with stored human gate state"),
	},
	{
		target: errs.ErrNotFound,
		result: eventconsumer.Poison("unknown_human_gate", "interaction response references an unknown human gate"),
	},
	{
		target: errs.ErrPreconditionFailed,
		result: eventconsumer.Poison("stale_human_gate_response", "interaction response cannot resolve current human gate state"),
	},
}

func interactionResponseConsumerError(err error) eventconsumer.Result {
	for _, candidate := range interactionResponseDomainErrorResults {
		if errors.Is(err, candidate.target) {
			return candidate.result
		}
	}
	return eventconsumer.Retry(err)
}
