package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
	governanceservice "github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/types/value"
)

const (
	interactionGateDecisionSourceService = "interaction-hub"
	interactionGateDecisionConsumerActor = "interaction-hub"
	interactionGateDecisionRequestKind   = "human_gate"
	interactionGateDecisionOwnerService  = "governance_manager"
	interactionGateDecisionAnswered      = "answered"
)

type gateDecisionRecorder interface {
	GetGateRequest(context.Context, governanceservice.GetGateRequestInput) (entity.GateRequest, error)
	SubmitGateDecision(context.Context, governanceservice.SubmitGateDecisionInput) (entity.GateDecision, entity.GateRequest, error)
}

func startInteractionGateDecisionConsumer(
	ctx context.Context,
	cfg Config,
	eventLogPool *pgxpool.Pool,
	recorder gateDecisionRecorder,
	logger *slog.Logger,
	errCh chan<- error,
) error {
	starter := interactionGateDecisionConsumerStarter{
		cfg:          cfg,
		eventLogPool: eventLogPool,
		recorder:     recorder,
	}
	return startGovernanceEventConsumer(
		ctx,
		cfg.InteractionGateDecisionConsumerEnabled,
		logger,
		errCh,
		"governance-manager interaction gate decision consumer starting",
		starter.build,
	)
}

type interactionGateDecisionConsumerStarter struct {
	cfg          Config
	eventLogPool *pgxpool.Pool
	recorder     gateDecisionRecorder
}

func (s interactionGateDecisionConsumerStarter) build(logger *slog.Logger) (*eventconsumer.Runner, error) {
	return newInteractionGateDecisionRunner(s.cfg, s.eventLogPool, s.recorder, logger)
}

func newInteractionGateDecisionRunner(cfg Config, eventLogPool *pgxpool.Pool, recorder gateDecisionRecorder, logger *slog.Logger) (*eventconsumer.Runner, error) {
	switch {
	case recorder == nil:
		return nil, fmt.Errorf("governance-manager interaction gate decision consumer requires gate decision recorder")
	case eventLogPool == nil:
		return nil, fmt.Errorf("governance-manager interaction gate decision consumer requires platform event-log database")
	}
	registry, err := eventconsumer.NewRegistry(eventconsumer.Registration{
		EventType:     interactionevents.EventRequestResponseRecorded,
		SchemaVersion: interactionevents.SchemaVersion,
		Handler:       interactionGateDecisionEventHandler{recorder: recorder},
	})
	if err != nil {
		return nil, fmt.Errorf("build interaction gate decision consumer registry: %w", err)
	}
	runner, err := eventconsumer.NewRunner(eventlog.NewStore(eventLogPool), registry, cfg.InteractionGateDecisionConsumerConfig(), logger, nil)
	if err != nil {
		return nil, fmt.Errorf("build interaction gate decision consumer runner: %w", err)
	}
	return runner, nil
}

type interactionGateDecisionEventHandler struct {
	recorder gateDecisionRecorder
}

func (h interactionGateDecisionEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	storedEvent := event.StoredEvent
	if strings.TrimSpace(storedEvent.SourceService) != interactionGateDecisionSourceService {
		return eventconsumer.Poison("invalid_source_service", "interaction gate decision event source service is not interaction-hub")
	}
	if strings.TrimSpace(storedEvent.AggregateType) != interactionevents.AggregateRequest {
		return eventconsumer.Poison("invalid_aggregate_type", "interaction gate decision event aggregate type is not request")
	}
	var payload interactionevents.Payload
	if err := json.Unmarshal(storedEvent.Payload, &payload); err != nil {
		return eventconsumer.Poison("invalid_payload", "interaction gate decision event payload is not valid interaction json")
	}
	if !interactionGateDecisionTargetsGovernance(payload) {
		return eventconsumer.Ack()
	}
	input, result := h.interactionGateDecisionInput(ctx, storedEvent, payload)
	if result.Status != "" {
		return result
	}
	if _, _, err := h.recorder.SubmitGateDecision(ctx, input); err != nil {
		return interactionGateDecisionConsumerError(err)
	}
	return eventconsumer.Ack()
}

func (h interactionGateDecisionEventHandler) interactionGateDecisionInput(ctx context.Context, storedEvent eventlog.StoredEvent, payload interactionevents.Payload) (governanceservice.SubmitGateDecisionInput, eventconsumer.Result) {
	if strings.TrimSpace(payload.Status) != interactionGateDecisionAnswered {
		return governanceservice.SubmitGateDecisionInput{}, eventconsumer.Poison("invalid_response_status", "interaction response event is not answered")
	}
	if strings.TrimSpace(payload.RequestID) == "" || strings.TrimSpace(payload.ResponseID) == "" {
		return governanceservice.SubmitGateDecisionInput{}, eventconsumer.Poison("missing_response_refs", "interaction response event misses request or response ref")
	}
	if strings.TrimSpace(payload.ActorRef) == "" {
		return governanceservice.SubmitGateDecisionInput{}, eventconsumer.Poison("missing_actor_ref", "interaction response event misses actor ref")
	}
	gateRequestID, parseErr := parseGateRequestID(payload.GovernanceGateRequestRef, payload.OwnerRequestRef)
	if parseErr != nil {
		return governanceservice.SubmitGateDecisionInput{}, eventconsumer.Poison("invalid_gate_request_ref", "interaction response event does not reference a governance gate request")
	}
	outcome, outcomeErr := interactionGateDecisionOutcome(payload.ResponseAction, payload.ResponseOutcome)
	if outcomeErr != nil {
		return governanceservice.SubmitGateDecisionInput{}, eventconsumer.Poison("unsupported_response_action", "interaction response action cannot resolve governance gate")
	}
	gate, loadErr := h.recorder.GetGateRequest(ctx, governanceservice.GetGateRequestInput{
		GateRequestID: gateRequestID,
		Meta: governanceservice.QueryMeta{
			Actor:     value.Actor{Type: "service", ID: interactionGateDecisionConsumerActor},
			RequestID: interactionGateDecisionRequestID(storedEvent),
		},
	})
	if loadErr != nil {
		return governanceservice.SubmitGateDecisionInput{}, interactionGateDecisionConsumerError(loadErr)
	}
	expectedVersion := gate.Version
	return governanceservice.SubmitGateDecisionInput{
		GateRequestID:          gateRequestID,
		DecisionActorRef:       strings.TrimSpace(payload.ActorRef),
		Outcome:                outcome,
		Reason:                 interactionGateDecisionReason(outcome),
		ConditionsSummary:      interactionGateDecisionConditions(payload),
		InteractionDeliveryRef: interactionGateDecisionDeliveryRef(payload),
		SourceRef:              interactionGateDecisionSourceRef(payload),
		Meta: governanceservice.CommandMeta{
			IdempotencyKey:  interactionGateDecisionIdempotencyKey(payload),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: interactionGateDecisionConsumerActor},
			RequestID:       interactionGateDecisionRequestID(storedEvent),
		},
	}, eventconsumer.Result{}
}

func interactionGateDecisionTargetsGovernance(payload interactionevents.Payload) bool {
	return strings.TrimSpace(payload.OwnerService) == interactionGateDecisionOwnerService &&
		strings.TrimSpace(payload.RequestKind) == interactionGateDecisionRequestKind
}

func interactionGateDecisionOutcome(action string, outcome string) (enum.GateOutcome, error) {
	normalizedAction := strings.TrimSpace(action)
	if normalizedAction == "" {
		return "", errs.ErrInvalidArgument
	}
	normalizedOutcome := strings.TrimSpace(outcome)
	if normalizedOutcome != "" && normalizedOutcome != normalizedAction {
		return "", errs.ErrInvalidArgument
	}
	switch normalizedAction {
	case string(enum.GateOutcomeApprove):
		return enum.GateOutcomeApprove, nil
	case string(enum.GateOutcomeReject):
		return enum.GateOutcomeReject, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func parseGateRequestID(refs ...string) (uuid.UUID, error) {
	for _, ref := range refs {
		for _, candidate := range externalRefIDCandidates(ref) {
			parsed, err := uuid.Parse(candidate)
			if err == nil && parsed != uuid.Nil {
				return parsed, nil
			}
		}
	}
	return uuid.Nil, errs.ErrInvalidArgument
}

func interactionGateDecisionDeliveryRef(payload interactionevents.Payload) value.InteractionDeliveryRef {
	return value.InteractionDeliveryRef{
		RequestRef:  interactionGateRef("request", firstNonBlank(payload.InteractionRequestRef, payload.RequestID)),
		DecisionRef: interactionGateRef("response", firstNonBlank(payload.OwnerDecisionRef, payload.InteractionResponseRef, payload.ResponseID)),
	}
}

func interactionGateDecisionSourceRef(payload interactionevents.Payload) string {
	return interactionGateRef("response", firstNonBlank(payload.ResponseSourceRef, payload.InteractionResponseRef, payload.ResponseID))
}

func interactionGateDecisionReason(outcome enum.GateOutcome) string {
	switch outcome {
	case enum.GateOutcomeApprove:
		return "owner approved governance gate via interaction-hub"
	case enum.GateOutcomeReject:
		return "owner rejected governance gate via interaction-hub"
	default:
		return "owner answered governance gate via interaction-hub"
	}
}

func interactionGateDecisionConditions(payload interactionevents.Payload) string {
	digest := firstNonBlank(payload.ResponseSummaryDigest, payload.ResponseObjectDigest)
	if digest == "" {
		return ""
	}
	return "interaction response digest " + digest
}

func interactionGateRef(kind string, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, ":") {
		return ref
	}
	return "interaction:" + kind + "/" + ref
}

func interactionGateDecisionRequestID(storedEvent eventlog.StoredEvent) string {
	if storedEvent.ID.String() == "" {
		return ""
	}
	return "interaction_event:" + storedEvent.ID.String()
}

func interactionGateDecisionIdempotencyKey(payload interactionevents.Payload) string {
	fingerprintBytes, err := json.Marshal(interactionGateDecisionFingerprint{
		RequestID:                strings.TrimSpace(payload.RequestID),
		ResponseID:               strings.TrimSpace(payload.ResponseID),
		RequestKind:              strings.TrimSpace(payload.RequestKind),
		OwnerService:             strings.TrimSpace(payload.OwnerService),
		DecisionOwnerKind:        strings.TrimSpace(payload.DecisionOwnerKind),
		GovernanceGateRequestRef: strings.TrimSpace(payload.GovernanceGateRequestRef),
		OwnerRequestRef:          strings.TrimSpace(payload.OwnerRequestRef),
		ResponseAction:           strings.TrimSpace(payload.ResponseAction),
		ResponseOutcome:          strings.TrimSpace(payload.ResponseOutcome),
		ActorRef:                 strings.TrimSpace(payload.ActorRef),
		InteractionRequestRef:    strings.TrimSpace(payload.InteractionRequestRef),
		InteractionResponseRef:   strings.TrimSpace(payload.InteractionResponseRef),
		ResponseSourceRef:        strings.TrimSpace(payload.ResponseSourceRef),
		ResponseSummaryDigest:    strings.TrimSpace(payload.ResponseSummaryDigest),
		ResponseObjectDigest:     strings.TrimSpace(payload.ResponseObjectDigest),
		Status:                   strings.TrimSpace(payload.Status),
		Version:                  payload.Version,
	})
	if err != nil {
		fingerprintBytes = []byte(strings.Join([]string{
			strings.TrimSpace(payload.RequestID),
			strings.TrimSpace(payload.ResponseID),
			strings.TrimSpace(payload.ResponseAction),
			strings.TrimSpace(payload.OwnerRequestRef),
		}, "\x00"))
	}
	sum := sha256.Sum256(fingerprintBytes)
	return "interaction_gate_decision:" + hex.EncodeToString(sum[:])
}

type interactionGateDecisionFingerprint struct {
	RequestID                string `json:"request_id"`
	ResponseID               string `json:"response_id"`
	RequestKind              string `json:"request_kind"`
	OwnerService             string `json:"owner_service,omitempty"`
	DecisionOwnerKind        string `json:"decision_owner_kind,omitempty"`
	GovernanceGateRequestRef string `json:"governance_gate_request_ref,omitempty"`
	OwnerRequestRef          string `json:"owner_request_ref,omitempty"`
	ResponseAction           string `json:"response_action,omitempty"`
	ResponseOutcome          string `json:"response_outcome,omitempty"`
	ActorRef                 string `json:"actor_ref,omitempty"`
	InteractionRequestRef    string `json:"interaction_request_ref,omitempty"`
	InteractionResponseRef   string `json:"interaction_response_ref,omitempty"`
	ResponseSourceRef        string `json:"response_source_ref,omitempty"`
	ResponseSummaryDigest    string `json:"response_summary_digest,omitempty"`
	ResponseObjectDigest     string `json:"response_object_digest,omitempty"`
	Status                   string `json:"status"`
	Version                  int64  `json:"version"`
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var interactionGateDecisionDomainErrorResults = governanceConsumerDomainErrors(
	eventConsumerPoison{code: "invalid_gate_decision_response", summary: "interaction response metadata is invalid"},
	eventConsumerPoison{code: "conflicting_gate_decision_response", summary: "interaction response conflicts with stored governance gate state"},
	eventConsumerPoison{code: "forbidden_gate_decision_response", summary: "interaction response actor is not authorized for governance gate"},
	eventConsumerPoison{code: "unknown_gate_request", summary: "interaction response references an unknown governance gate request"},
	eventConsumerPoison{code: "stale_gate_decision_response", summary: "interaction response cannot resolve current governance gate state"},
)

func interactionGateDecisionConsumerError(err error) eventconsumer.Result {
	return governanceConsumerError(err, interactionGateDecisionDomainErrorResults)
}
