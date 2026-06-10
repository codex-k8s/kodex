package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	governanceevents "github.com/codex-k8s/kodex/libs/go/platformevents/governance"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	selfDeployGateDecisionSourceService = "governance-manager"
	selfDeployGateDecisionConsumerActor = "governance-manager"
	selfDeployGateDecisionTargetType    = "self_deploy_plan"
	selfDeployGateDecisionResolved      = "resolved"
	selfDeployGateDecisionTargetPrefix  = "agent:self-deploy-plan:"
)

type selfDeployGateDecisionRecorder interface {
	GetSelfDeployPlan(context.Context, uuid.UUID) (entity.SelfDeployPlan, error)
	RecordSelfDeployPlanGateDecision(context.Context, agentservice.RecordSelfDeployPlanGateDecisionInput) (entity.SelfDeployPlan, error)
}

type selfDeployGateDecisionConsumerStarter struct {
	cfg          Config
	eventLogPool *pgxpool.Pool
	recorder     selfDeployGateDecisionRecorder
	logger       *slog.Logger
	errCh        chan<- error
}

func (s selfDeployGateDecisionConsumerStarter) start(ctx context.Context) error {
	return startManagedEventConsumerWithParts(ctx, s.cfg.SelfDeployGateDecisionConsumerEnabled, s.logger, s.errCh, s.validate, s.runner, runSelfDeployGateDecisionConsumer)
}

func (s selfDeployGateDecisionConsumerStarter) validate() error {
	switch {
	case s.recorder == nil:
		return fmt.Errorf("agent-manager self-deploy gate decision consumer requires self-deploy plan recorder")
	case s.eventLogPool == nil:
		return fmt.Errorf("agent-manager self-deploy gate decision consumer requires platform event-log database")
	default:
		return nil
	}
}

func (s selfDeployGateDecisionConsumerStarter) runner(logger *slog.Logger) (*eventconsumer.Runner, error) {
	return newEventConsumerRunner(s.eventLogPool, selfDeployGateDecisionConsumerRegistration(s.recorder), s.cfg.SelfDeployGateDecisionConsumerConfig(), logger)
}

func selfDeployGateDecisionConsumerRegistration(recorder selfDeployGateDecisionRecorder) eventconsumer.Registration {
	return eventconsumer.Registration{
		EventType:     governanceevents.EventGateResolved,
		SchemaVersion: governanceevents.SchemaVersion,
		Handler:       selfDeployGateDecisionEventHandler{recorder: recorder},
	}
}

func runSelfDeployGateDecisionConsumer(ctx context.Context, runner *eventconsumer.Runner, logger *slog.Logger, errCh chan<- error) {
	logger.Info("agent-manager self-deploy gate decision consumer starting")
	if err := runner.Run(ctx); err != nil {
		errCh <- err
	}
}

type selfDeployGateDecisionEventHandler struct {
	recorder selfDeployGateDecisionRecorder
}

var selfDeployGateDecisionDecodeConfig = typedEventDecodeConfig{
	sourceService:    selfDeployGateDecisionSourceService,
	invalidSource:    eventconsumer.Poison("invalid_source_service", "self-deploy gate decision event source service is not governance-manager"),
	aggregateType:    governanceevents.AggregateGate,
	invalidAggregate: eventconsumer.Poison("invalid_aggregate_type", "self-deploy gate decision aggregate type is not gate"),
	invalidPayload:   eventconsumer.Poison("invalid_payload", "self-deploy gate decision payload is not valid governance json"),
}

func (h selfDeployGateDecisionEventHandler) HandleEvent(ctx context.Context, event eventconsumer.Event) eventconsumer.Result {
	return handleTypedEventCommand(ctx, event, selfDeployGateDecisionDecodeConfig, selfDeployGateDecisionTargetsSelfDeployPlan, h.selfDeployGateDecisionInput, h.recordSelfDeployPlanGateDecision, selfDeployGateDecisionConsumerError)
}

func (h selfDeployGateDecisionEventHandler) selfDeployGateDecisionInput(ctx context.Context, payload governanceevents.Payload) (agentservice.RecordSelfDeployPlanGateDecisionInput, eventconsumer.Result) {
	if strings.TrimSpace(payload.Status) != selfDeployGateDecisionResolved {
		return agentservice.RecordSelfDeployPlanGateDecisionInput{}, eventconsumer.Poison("stale_self_deploy_gate_status", "self-deploy gate decision event is not resolved")
	}
	if strings.TrimSpace(payload.GateRequestID) == "" || strings.TrimSpace(payload.GateDecisionID) == "" {
		return agentservice.RecordSelfDeployPlanGateDecisionInput{}, eventconsumer.Poison("missing_self_deploy_gate_refs", "self-deploy gate decision event misses gate request or decision ref")
	}
	if payload.Version < 1 {
		return agentservice.RecordSelfDeployPlanGateDecisionInput{}, eventconsumer.Poison("invalid_self_deploy_gate_version", "self-deploy gate decision event version is invalid")
	}
	planID, err := parseSelfDeployGateDecisionTargetRef(payload.TargetRef)
	if err != nil {
		return agentservice.RecordSelfDeployPlanGateDecisionInput{}, eventconsumer.Poison("invalid_self_deploy_gate_target", "self-deploy gate decision target ref is invalid")
	}
	outcome, err := selfDeployGateDecisionOutcome(payload.Outcome)
	if err != nil {
		return agentservice.RecordSelfDeployPlanGateDecisionInput{}, eventconsumer.Poison("unsupported_self_deploy_gate_outcome", "self-deploy gate decision outcome is not supported")
	}
	plan, err := h.recorder.GetSelfDeployPlan(ctx, planID)
	if err != nil {
		return agentservice.RecordSelfDeployPlanGateDecisionInput{}, selfDeployGateDecisionConsumerError(err)
	}
	expectedVersion := plan.Version
	return agentservice.RecordSelfDeployPlanGateDecisionInput{
		Meta: value.CommandMeta{
			IdempotencyKey:  selfDeployGateDecisionIdempotencyKey(payload.GateDecisionID),
			ExpectedVersion: &expectedVersion,
			Actor:           value.Actor{Type: "service", ID: selfDeployGateDecisionConsumerActor},
		},
		SelfDeployPlanID: planID,
		GateRequestRef:   governanceGateRef("gate_request", payload.GateRequestID),
		GateDecisionRef:  governanceGateRef("gate_decision", payload.GateDecisionID),
		Outcome:          outcome,
		SafeSummary:      strings.TrimSpace(payload.SafeSummary),
	}, eventconsumer.Result{}
}

func selfDeployGateDecisionTargetsSelfDeployPlan(payload governanceevents.Payload) bool {
	return strings.TrimSpace(payload.TargetType) == selfDeployGateDecisionTargetType
}

func parseSelfDeployGateDecisionTargetRef(ref string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, selfDeployGateDecisionTargetPrefix) {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	id, err := uuid.Parse(strings.TrimSpace(strings.TrimPrefix(trimmed, selfDeployGateDecisionTargetPrefix)))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return id, nil
}

func selfDeployGateDecisionOutcome(outcome string) (agentservice.SelfDeployPlanGateDecisionOutcome, error) {
	switch strings.TrimSpace(outcome) {
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeApprove):
		return agentservice.SelfDeployPlanGateDecisionOutcomeApprove, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeApproveWithConditions):
		return agentservice.SelfDeployPlanGateDecisionOutcomeApproveWithConditions, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeReject):
		return agentservice.SelfDeployPlanGateDecisionOutcomeReject, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeRevise):
		return agentservice.SelfDeployPlanGateDecisionOutcomeRevise, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeRequestChanges):
		return agentservice.SelfDeployPlanGateDecisionOutcomeRequestChanges, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeHold):
		return agentservice.SelfDeployPlanGateDecisionOutcomeHold, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeRollback):
		return agentservice.SelfDeployPlanGateDecisionOutcomeRollback, nil
	case string(agentservice.SelfDeployPlanGateDecisionOutcomeEscalate):
		return agentservice.SelfDeployPlanGateDecisionOutcomeEscalate, nil
	default:
		return "", errs.ErrInvalidArgument
	}
}

func selfDeployGateDecisionIdempotencyKey(decisionID string) string {
	return "governance_gate_resolved:" + strings.TrimSpace(decisionID)
}

func governanceGateRef(kind string, id string) string {
	return "governance:" + strings.TrimSpace(kind) + "/" + strings.TrimSpace(id)
}

func (h selfDeployGateDecisionEventHandler) recordSelfDeployPlanGateDecision(ctx context.Context, input agentservice.RecordSelfDeployPlanGateDecisionInput) error {
	_, err := h.recorder.RecordSelfDeployPlanGateDecision(ctx, input)
	return err
}

func selfDeployGateDecisionConsumerError(err error) eventconsumer.Result {
	return commonEventConsumerDomainError(
		err,
		eventconsumer.Poison("invalid_self_deploy_gate_decision", "self-deploy gate decision metadata is invalid"),
		eventconsumer.Poison("conflicting_self_deploy_gate_decision", "self-deploy gate decision conflicts with stored plan state"),
		eventconsumer.Poison("unknown_self_deploy_plan", "self-deploy gate decision references an unknown plan"),
		eventconsumer.Poison("stale_self_deploy_gate_decision", "self-deploy gate decision cannot resolve current plan state"),
	)
}
