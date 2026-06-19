package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const selfDeployRuntimeReconcilePageSize int32 = 50

type selfDeployRuntimeEnsurer interface {
	ListSelfDeployPlans(context.Context, agentservice.SelfDeployPlanList) ([]entity.SelfDeployPlan, value.PageResult, error)
	EnsureSelfDeployPlanRuntime(context.Context, agentservice.EnsureSelfDeployPlanRuntimeInput) (entity.SelfDeployPlan, error)
}

func startSelfDeployRuntimeReconciler(ctx context.Context, cfg Config, service selfDeployRuntimeEnsurer, logger *slog.Logger) error {
	if !cfg.SelfDeployBuildDispatchEnabled || !cfg.SelfDeploySignalConsumerEnabled {
		return nil
	}
	projectRef := strings.TrimSpace(cfg.SelfDeploySignalConsumerProjectID)
	if projectRef == "" {
		return nil
	}
	go runSelfDeployProjectReconciler(
		ctx,
		projectRef,
		logger,
		cfg.SelfDeploySignalConsumerMaxAttempts,
		cfg.SelfDeploySignalConsumerRetryInitialDelay,
		cfg.SelfDeploySignalConsumerRetryMaxDelay,
		"agent-manager self-deploy runtime reconcile failed",
		selfDeployRuntimeReconcileErrorCode,
		func(ctx context.Context, projectRef string) error {
			return reconcileSelfDeployPlanRuntime(ctx, service, projectRef)
		},
	)
	return nil
}

func reconcileSelfDeployPlanRuntime(ctx context.Context, service selfDeployRuntimeEnsurer, projectRef string) error {
	status := enum.SelfDeployPlanStatusApproved
	page := value.PageRequest{PageSize: selfDeployRuntimeReconcilePageSize}
	var firstErr error
	for {
		plans, result, err := service.ListSelfDeployPlans(ctx, agentservice.SelfDeployPlanList{
			ProjectRef: strings.TrimSpace(projectRef),
			Status:     &status,
			Page:       page,
		})
		if err != nil {
			return selfDeployRuntimeReconcileError{code: "plan_list_failed", err: err}
		}
		for _, plan := range plans {
			if !agentservice.SelfDeployPlanNeedsRuntimeRecovery(plan) {
				continue
			}
			_, err := service.EnsureSelfDeployPlanRuntime(ctx, agentservice.EnsureSelfDeployPlanRuntimeInput{
				Meta: value.CommandMeta{
					IdempotencyKey: "self_deploy_plan_runtime_reconcile:" + plan.ID.String(),
					Actor:          value.Actor{Type: "service", ID: "agent-manager"},
				},
				SelfDeployPlanID: plan.ID,
			})
			if err != nil && firstErr == nil {
				firstErr = selfDeployRuntimeReconcileError{code: "runtime_reconcile_failed", err: err}
			}
		}
		if strings.TrimSpace(result.NextPageToken) == "" {
			return firstErr
		}
		page.PageToken = result.NextPageToken
	}
}

func selfDeployRuntimeReconcileErrorCode(err error) string {
	var reconcileErr selfDeployRuntimeReconcileError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled):
		return "context_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.As(err, &reconcileErr):
		return reconcileErr.code
	default:
		return "runtime_reconcile_failed"
	}
}

type selfDeployRuntimeReconcileError struct {
	code string
	err  error
}

func (e selfDeployRuntimeReconcileError) Error() string {
	return e.code
}

func (e selfDeployRuntimeReconcileError) Unwrap() error {
	return e.err
}
