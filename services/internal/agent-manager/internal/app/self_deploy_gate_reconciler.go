package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const selfDeployGateReconcilePageSize int32 = 50

type selfDeployGateEnsurer interface {
	ListSelfDeployPlans(context.Context, agentservice.SelfDeployPlanList) ([]entity.SelfDeployPlan, value.PageResult, error)
	EnsureSelfDeployPlanGovernanceGate(context.Context, agentservice.EnsureSelfDeployPlanGovernanceGateInput) (entity.SelfDeployPlan, error)
}

func startSelfDeployGateReconciler(ctx context.Context, cfg Config, service selfDeployGateEnsurer, logger *slog.Logger) error {
	if !cfg.SelfDeployGovernanceGateEnabled || !cfg.SelfDeploySignalConsumerEnabled {
		return nil
	}
	projectRef := strings.TrimSpace(cfg.SelfDeploySignalConsumerProjectID)
	if projectRef == "" {
		return nil
	}
	go runSelfDeployGateReconciler(
		ctx,
		service,
		projectRef,
		logger,
		cfg.SelfDeploySignalConsumerMaxAttempts,
		cfg.SelfDeploySignalConsumerRetryInitialDelay,
		cfg.SelfDeploySignalConsumerRetryMaxDelay,
	)
	return nil
}

func runSelfDeployGateReconciler(
	ctx context.Context,
	service selfDeployGateEnsurer,
	projectRef string,
	logger *slog.Logger,
	maxAttempts int,
	initialDelay time.Duration,
	maxDelay time.Duration,
) {
	runSelfDeployProjectReconciler(
		ctx,
		projectRef,
		logger,
		maxAttempts,
		initialDelay,
		maxDelay,
		"agent-manager self-deploy gate reconcile failed",
		selfDeployGateReconcileErrorCode,
		func(ctx context.Context, projectRef string) error {
			return reconcileSelfDeployPlanGovernanceGates(ctx, service, projectRef)
		},
	)
}

func runSelfDeployProjectReconciler(
	ctx context.Context,
	projectRef string,
	logger *slog.Logger,
	maxAttempts int,
	initialDelay time.Duration,
	maxDelay time.Duration,
	logMessage string,
	errorCode func(error) string,
	reconcile func(context.Context, string) error,
) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if initialDelay <= 0 {
		initialDelay = time.Second
	}
	if maxDelay < initialDelay {
		maxDelay = initialDelay
	}
	if logger == nil {
		logger = slog.Default()
	}
	delay := initialDelay
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := reconcile(ctx, projectRef)
		if err == nil {
			return
		}
		logger.Warn(logMessage, "attempt", attempt, "error_code", errorCode(err))
		if attempt == maxAttempts {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

func reconcileSelfDeployPlanGovernanceGates(ctx context.Context, service selfDeployGateEnsurer, projectRef string) error {
	status := enum.SelfDeployPlanStatusPendingApproval
	page := value.PageRequest{PageSize: selfDeployGateReconcilePageSize}
	var firstErr error
	for {
		plans, result, err := service.ListSelfDeployPlans(ctx, agentservice.SelfDeployPlanList{
			ProjectRef: strings.TrimSpace(projectRef),
			Status:     &status,
			Page:       page,
		})
		if err != nil {
			return selfDeployGateReconcileError{code: "plan_list_failed", err: err}
		}
		for _, plan := range plans {
			if !selfDeployPlanNeedsGovernanceGateEnsure(plan) {
				continue
			}
			_, err := service.EnsureSelfDeployPlanGovernanceGate(ctx, agentservice.EnsureSelfDeployPlanGovernanceGateInput{
				Meta: value.CommandMeta{
					IdempotencyKey: "self_deploy_plan_gate_reconcile:" + plan.ID.String(),
					Actor:          value.Actor{Type: "service", ID: "agent-manager"},
				},
				SelfDeployPlanID: plan.ID,
			})
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if strings.TrimSpace(result.NextPageToken) == "" {
			return firstErr
		}
		page.PageToken = result.NextPageToken
	}
}

func selfDeployPlanNeedsGovernanceGateEnsure(plan entity.SelfDeployPlan) bool {
	return plan.ID != uuid.Nil &&
		plan.Status == enum.SelfDeployPlanStatusPendingApproval &&
		strings.TrimSpace(plan.GovernanceContext.GateDecisionRef) == ""
}

func selfDeployGateReconcileErrorCode(err error) string {
	var reconcileErr selfDeployGateReconcileError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.As(err, &reconcileErr):
		return reconcileErr.code
	case agentservice.SelfDeployGateRecoveryErrorCode(err) != "":
		return agentservice.SelfDeployGateRecoveryErrorCode(err)
	default:
		return "reconcile_failed"
	}
}

type selfDeployGateReconcileError struct {
	code string
	err  error
}

func (e selfDeployGateReconcileError) Error() string {
	return e.code
}

func (e selfDeployGateReconcileError) Unwrap() error {
	return e.err
}
