package app

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

func TestReconcileSelfDeployPlanGovernanceGatesEnsuresPendingPlanWithoutGateRef(t *testing.T) {
	t.Parallel()

	projectRef := "63135040-fe44-4ec4-83d5-b0126dc23b32"
	planID := uuid.MustParse("aeca8f7c-6e2b-4709-b944-453e85434aeb")
	service := &fakeSelfDeployGateEnsureService{plans: []entity.SelfDeployPlan{{
		VersionedBase: entity.VersionedBase{ID: planID, Version: 1},
		ProjectRef:    projectRef,
		Status:        enum.SelfDeployPlanStatusPendingApproval,
	}}}

	err := reconcileSelfDeployPlanGovernanceGates(context.Background(), service, projectRef)
	if err != nil {
		t.Fatalf("reconcileSelfDeployPlanGovernanceGates() err = %v", err)
	}
	if service.listInput.ProjectRef != projectRef ||
		service.listInput.Status == nil ||
		*service.listInput.Status != enum.SelfDeployPlanStatusPendingApproval ||
		service.listInput.Page.PageSize != selfDeployGateReconcilePageSize {
		t.Fatalf("list input = %+v", service.listInput)
	}
	if len(service.ensureInputs) != 1 || service.ensureInputs[0].SelfDeployPlanID != planID {
		t.Fatalf("ensure inputs = %+v", service.ensureInputs)
	}
	if service.ensureInputs[0].Meta.Actor.Type != "service" ||
		service.ensureInputs[0].Meta.Actor.ID != "agent-manager" ||
		service.ensureInputs[0].Meta.IdempotencyKey == "" {
		t.Fatalf("ensure meta = %+v", service.ensureInputs[0].Meta)
	}
}

func TestReconcileSelfDeployPlanGovernanceGatesEnsuresPendingPlanWithoutDecisionRef(t *testing.T) {
	t.Parallel()

	projectRef := "63135040-fe44-4ec4-83d5-b0126dc23b32"
	planID := uuid.MustParse("7b24d574-902b-4d1e-b7cb-ab57967688a1")
	service := &fakeSelfDeployGateEnsureService{plans: []entity.SelfDeployPlan{{
		VersionedBase: entity.VersionedBase{ID: planID, Version: 2},
		ProjectRef:    projectRef,
		Status:        enum.SelfDeployPlanStatusPendingApproval,
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
		},
	}}}

	err := reconcileSelfDeployPlanGovernanceGates(context.Background(), service, projectRef)
	if err != nil {
		t.Fatalf("reconcileSelfDeployPlanGovernanceGates() err = %v", err)
	}
	if len(service.ensureInputs) != 1 || service.ensureInputs[0].SelfDeployPlanID != planID {
		t.Fatalf("ensure inputs = %+v, want pending plan without decision ref", service.ensureInputs)
	}
}

func TestReconcileSelfDeployPlanGovernanceGatesSkipsPlanWithDecisionRef(t *testing.T) {
	t.Parallel()

	projectRef := "63135040-fe44-4ec4-83d5-b0126dc23b32"
	service := &fakeSelfDeployGateEnsureService{plans: []entity.SelfDeployPlan{{
		VersionedBase: entity.VersionedBase{ID: uuid.New(), Version: 3},
		ProjectRef:    projectRef,
		Status:        enum.SelfDeployPlanStatusPendingApproval,
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef:  "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
			GateDecisionRef: "governance:gate_decision/cccccccc-cccc-4ccc-cccc-cccccccccccc",
		},
	}}}

	err := reconcileSelfDeployPlanGovernanceGates(context.Background(), service, projectRef)
	if err != nil {
		t.Fatalf("reconcileSelfDeployPlanGovernanceGates() err = %v", err)
	}
	if len(service.ensureInputs) != 0 {
		t.Fatalf("ensure inputs = %+v, want empty", service.ensureInputs)
	}
}

func TestSelfDeployGateReconcileErrorCodeReportsPlanListFailure(t *testing.T) {
	t.Parallel()

	err := selfDeployGateReconcileError{code: "plan_list_failed", err: errors.New("database unavailable")}
	if code := selfDeployGateReconcileErrorCode(err); code != "plan_list_failed" {
		t.Fatalf("error code = %q, want plan_list_failed", code)
	}
}

func TestReconcileSelfDeployPlanRuntimeRetriesApprovedPermissionDenied(t *testing.T) {
	t.Parallel()

	projectRef := "63135040-fe44-4ec4-83d5-b0126dc23b32"
	planID := uuid.MustParse("5e25c330-bdf8-4479-ad8a-8e9893b4c5d0")
	service := &fakeSelfDeployGateEnsureService{plans: []entity.SelfDeployPlan{{
		VersionedBase: entity.VersionedBase{ID: planID, Version: 7},
		ProjectRef:    projectRef,
		Status:        enum.SelfDeployPlanStatusApproved,
		GovernanceContext: value.GovernanceContextRef{
			GateDecisionRef: "governance:gate_decision/91217d55-63f9-476b-a507-61aeb0be8102",
		},
		ExpectedRuntimeJobTypes: []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild},
		RuntimeBuildStatus:      enum.SelfDeployRuntimeBuildStatusFailed,
		RuntimeBuildErrorCode:   "permission_denied",
	}}}

	err := reconcileSelfDeployPlanRuntime(context.Background(), service, projectRef)
	if err != nil {
		t.Fatalf("reconcileSelfDeployPlanRuntime() err = %v", err)
	}
	if service.listInput.ProjectRef != projectRef ||
		service.listInput.Status == nil ||
		*service.listInput.Status != enum.SelfDeployPlanStatusApproved ||
		service.listInput.Page.PageSize != selfDeployRuntimeReconcilePageSize {
		t.Fatalf("list input = %+v", service.listInput)
	}
	if len(service.ensureRuntimeInputs) != 1 || service.ensureRuntimeInputs[0].SelfDeployPlanID != planID {
		t.Fatalf("ensure runtime inputs = %+v", service.ensureRuntimeInputs)
	}
	if len(service.ensureInputs) != 0 {
		t.Fatalf("governance gate ensure inputs = %+v, want none for approved runtime recovery", service.ensureInputs)
	}
	if service.ensureRuntimeInputs[0].Meta.IdempotencyKey != "self_deploy_plan_runtime_reconcile:"+planID.String() ||
		service.ensureRuntimeInputs[0].Meta.Actor.Type != "service" ||
		service.ensureRuntimeInputs[0].Meta.Actor.ID != "agent-manager" {
		t.Fatalf("ensure runtime meta = %+v", service.ensureRuntimeInputs[0].Meta)
	}
}

func TestReconcileSelfDeployPlanRuntimeSkipsTerminalPolicyStale(t *testing.T) {
	t.Parallel()

	projectRef := "63135040-fe44-4ec4-83d5-b0126dc23b32"
	service := &fakeSelfDeployGateEnsureService{plans: []entity.SelfDeployPlan{{
		VersionedBase: entity.VersionedBase{ID: uuid.New(), Version: 8},
		ProjectRef:    projectRef,
		Status:        enum.SelfDeployPlanStatusApproved,
		GovernanceContext: value.GovernanceContextRef{
			GateDecisionRef: "governance:gate_decision/91217d55-63f9-476b-a507-61aeb0be8102",
		},
		ExpectedRuntimeJobTypes: []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild},
		RuntimeBuildStatus:      enum.SelfDeployRuntimeBuildStatusBlocked,
		RuntimeBuildErrorCode:   "policy_stale",
	}}}

	err := reconcileSelfDeployPlanRuntime(context.Background(), service, projectRef)
	if err != nil {
		t.Fatalf("reconcileSelfDeployPlanRuntime() err = %v", err)
	}
	if len(service.ensureRuntimeInputs) != 0 {
		t.Fatalf("ensure runtime inputs = %+v, want none for terminal policy_stale", service.ensureRuntimeInputs)
	}
}

func TestSelfDeployRuntimeReconcileErrorCodeReportsRuntimeFailure(t *testing.T) {
	t.Parallel()

	err := selfDeployRuntimeReconcileError{code: "runtime_reconcile_failed", err: errors.New("runtime unavailable")}
	if code := selfDeployRuntimeReconcileErrorCode(err); code != "runtime_reconcile_failed" {
		t.Fatalf("error code = %q, want runtime_reconcile_failed", code)
	}
}

type fakeSelfDeployGateEnsureService struct {
	plans               []entity.SelfDeployPlan
	page                value.PageResult
	listInput           agentservice.SelfDeployPlanList
	ensureInputs        []agentservice.EnsureSelfDeployPlanGovernanceGateInput
	ensureRuntimeInputs []agentservice.EnsureSelfDeployPlanRuntimeInput
	ensureErr           error
	ensureRuntimeErr    error
}

func (f *fakeSelfDeployGateEnsureService) ListSelfDeployPlans(_ context.Context, input agentservice.SelfDeployPlanList) ([]entity.SelfDeployPlan, value.PageResult, error) {
	f.listInput = input
	return f.plans, f.page, nil
}

func (f *fakeSelfDeployGateEnsureService) EnsureSelfDeployPlanGovernanceGate(_ context.Context, input agentservice.EnsureSelfDeployPlanGovernanceGateInput) (entity.SelfDeployPlan, error) {
	f.ensureInputs = append(f.ensureInputs, input)
	if f.ensureErr != nil {
		return entity.SelfDeployPlan{}, f.ensureErr
	}
	return entity.SelfDeployPlan{VersionedBase: entity.VersionedBase{ID: input.SelfDeployPlanID}}, nil
}

func (f *fakeSelfDeployGateEnsureService) EnsureSelfDeployPlanRuntime(_ context.Context, input agentservice.EnsureSelfDeployPlanRuntimeInput) (entity.SelfDeployPlan, error) {
	f.ensureRuntimeInputs = append(f.ensureRuntimeInputs, input)
	if f.ensureRuntimeErr != nil {
		return entity.SelfDeployPlan{}, f.ensureRuntimeErr
	}
	return entity.SelfDeployPlan{VersionedBase: entity.VersionedBase{ID: input.SelfDeployPlanID}}, nil
}
