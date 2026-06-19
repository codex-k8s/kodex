package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"google.golang.org/grpc"
)

func TestObserveSelfDeployChainPendingApproval(t *testing.T) {
	plan := testPlan(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL)
	clients := chainClients{
		AgentManager: &fakeAgentManager{
			listResponse: &agentsv1.ListSelfDeployPlansResponse{SelfDeployPlans: []*agentsv1.SelfDeployPlan{plan}},
		},
		ProjectCatalog: &fakeProjectCatalog{response: &projectsv1.SelfDeploySignalResponse{
			Status: projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY,
			Signal: &projectsv1.SelfDeploySignal{
				ProviderSignalRef:         "provider:signal:1",
				ProjectRef:                "project:platform",
				RepositoryRef:             "repository:kodex",
				ProjectSignalFingerprint:  "sha256:project-signal",
				ProviderChangeFingerprint: "sha256:provider-change",
				SafeSummary:               "signal ready",
				Version:                   3,
			},
		}},
		GovernanceManager: &fakeGovernanceManager{response: &governancev1.GovernanceSummaryResponse{Summary: &governancev1.GovernanceSummary{
			PendingDecisions: []*governancev1.GovernanceDecisionSummary{{
				Kind:              governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_REQUEST,
				Attention:         governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_PENDING,
				Id:                "gate-1",
				GateRequestStatus: governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION,
				SafeSummary:       "owner approval required",
				Version:           7,
			}},
		}}},
		RuntimeManager: &fakeRuntimeManager{},
		StaffGateway: &fakeStaffGateway{response: &staffSummaryResponse{RequestID: "req-1", Summary: staffDeploySummary{
			Availability: "ready",
			ChainStatus:  "governance_gate_pending",
			NextStep:     staffNextStep{Code: "review_governance_gate", Summary: "owner decision pending"},
			DeployPlan:   staffDeployPlan{Status: "pending_approval"},
			Governance:   staffGovernanceSummary{Status: "pending", GateRequestRef: stringPtr("gate-1")},
			Runtime:      staffRuntimeSummary{Status: "pending"},
		}}},
	}

	report, err := observeSelfDeployChain(context.Background(), testOptions(), clients, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("observeSelfDeployChain(): %v", err)
	}
	if report.Status != reportStatusWaiting || report.CurrentStage != stageSelfDeployPlan {
		t.Fatalf("status/current_stage = %s/%s, want waiting/%s", report.Status, report.CurrentStage, stageSelfDeployPlan)
	}
	if got := stageByName(report, stageGateDecision); got.Status != stageStatusWaiting || got.Code != "owner_decision_pending" {
		t.Fatalf("gate decision stage = %#v, want owner_decision_pending", got)
	}
	if got := stageByName(report, stageBuildJob); got.Status != stageStatusWaiting || got.Code != "owner_decision_pending" {
		t.Fatalf("build job stage = %#v, want waiting before approval", got)
	}
}

func TestObserveSelfDeployChainRuntimeBuildBlocker(t *testing.T) {
	plan := testPlan(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED)
	plan.GovernanceContext.GateDecisionRef = stringPtr("gate-decision-1")
	plan.RuntimeBuildStatus = agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_FAILED
	plan.RuntimeBuildErrorCode = stringPtr("permission_denied")
	plan.RuntimeBuildSummary = stringPtr("runtime service actor lacks job create grant")
	plan.RuntimeBuildContexts = []*agentsv1.SelfDeployRuntimeBuildContextRef{{
		ServiceKey:                 "agent-manager",
		RuntimeBuildContextRef:     stringPtr("build-context-1"),
		RuntimeBuildContextStatus:  stringPtr("ready"),
		MaterializationFingerprint: stringPtr("sha256:context-fingerprint"),
		BuildPlanItemFingerprint:   stringPtr("sha256:build-item"),
		BuildContextDigest:         stringPtr("sha256:build-context"),
		ManifestBundleDigest:       stringPtr("sha256:bundle"),
	}}
	plan.RuntimeBuildJobs = []*agentsv1.SelfDeployRuntimeBuildJobRef{{
		ServiceKey:               "agent-manager",
		RuntimeJobRef:            "runtime-job-build-1",
		RuntimeJobStatus:         stringPtr("failed"),
		BuildPlanItemFingerprint: stringPtr("sha256:build-item"),
	}}
	clients := chainClients{
		AgentManager: &fakeAgentManager{getResponse: &agentsv1.SelfDeployPlanResponse{SelfDeployPlan: plan}},
		ProjectCatalog: &fakeProjectCatalog{response: &projectsv1.SelfDeploySignalResponse{
			Status: projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY,
			Signal: &projectsv1.SelfDeploySignal{ProviderSignalRef: "provider:signal:1", ProjectRef: "project:platform", RepositoryRef: "repository:kodex"},
		}},
		GovernanceManager: &fakeGovernanceManager{response: &governancev1.GovernanceSummaryResponse{Summary: &governancev1.GovernanceSummary{
			CompletedDecisions: []*governancev1.GovernanceDecisionSummary{{
				Kind:        governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_DECISION,
				Attention:   governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_INFORMATIONAL,
				Id:          "gate-decision-1",
				GateOutcome: governancev1.GateOutcome_GATE_OUTCOME_APPROVE,
				SafeSummary: "approved",
				Version:     8,
			}},
		}}},
		RuntimeManager: &fakeRuntimeManager{
			buildContextResponse: &runtimev1.BuildContextResponse{BuildContext: &runtimev1.BuildContext{
				BuildContextId:     "build-context-1",
				Status:             runtimev1.BuildContextStatus_BUILD_CONTEXT_STATUS_READY,
				ContextFingerprint: "sha256:context-fingerprint",
				BuildContextDigest: "sha256:build-context",
				NextAction:         "wait build job",
				Version:            2,
			}},
			jobResponses: map[string]*runtimev1.JobResponse{
				"runtime-job-build-1": {Job: &runtimev1.Job{
					JobId:            "runtime-job-build-1",
					Status:           runtimev1.JobStatus_JOB_STATUS_FAILED,
					LastErrorCode:    "permission_denied",
					LastErrorMessage: "service actor denied",
					Version:          4,
				}},
			},
		},
		StaffGateway: &fakeStaffGateway{response: &staffSummaryResponse{RequestID: "req-1", Summary: staffDeploySummary{
			Availability: "ready",
			ChainStatus:  "blocked",
			NextStep:     staffNextStep{Code: "inspect_blocker", Summary: "runtime blocked"},
			DeployPlan:   staffDeployPlan{Status: "approved"},
			Governance:   staffGovernanceSummary{Status: "resolved", GateDecisionRef: stringPtr("gate-decision-1")},
			Runtime:      staffRuntimeSummary{Status: "failed", RuntimeJobRef: stringPtr("runtime-job-build-1")},
			SafeError:    &staffSafeError{Code: "permission_denied", Summary: "runtime blocked"},
		}}},
	}
	options := testOptions()
	options.SelfDeployPlanID = "plan-1"

	report, err := observeSelfDeployChain(context.Background(), options, clients, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("observeSelfDeployChain(): %v", err)
	}
	if report.Status != reportStatusBlocked || report.Blocker == nil || report.Blocker.Code != "permission_denied" {
		t.Fatalf("report blocker = %#v, status=%s; want permission_denied", report.Blocker, report.Status)
	}
	if got := stageByName(report, stageBuildJob); got.Status != stageStatusBlocked || got.Items[0].Summary == "" {
		t.Fatalf("build job stage = %#v, want blocked safe summary", got)
	}
	if strings.Contains(strings.ToLower(report.Blocker.Summary), "payload") {
		t.Fatalf("blocker summary leaked raw wording: %q", report.Blocker.Summary)
	}
}

func TestHTTPStaffSummaryClientUsesSafeHeadersAndDTO(t *testing.T) {
	var actorType string
	var actorID string
	var requestID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		actorType = req.Header.Get("X-Kodex-Actor-Type")
		actorID = req.Header.Get("X-Kodex-Actor-Id")
		requestID = req.Header.Get("X-Kodex-Request-Id")
		if got := req.URL.Query().Get("project_ref"); got != "project:platform" {
			t.Fatalf("project_ref = %q, want project:platform", got)
		}
		_ = json.NewEncoder(w).Encode(staffSummaryResponse{
			RequestID: "req-1",
			Summary: staffDeploySummary{
				Availability: "ready",
				ChainStatus:  "approved_ready_for_build",
				NextStep:     staffNextStep{Code: "ready_for_build", Summary: "ready"},
				DeployPlan:   staffDeployPlan{Status: "approved"},
				Governance:   staffGovernanceSummary{Status: "resolved"},
				Runtime:      staffRuntimeSummary{Status: "pending"},
			},
		})
	}))
	defer server.Close()
	options := testOptions()
	options.StaffGatewayURL = server.URL

	response, err := (httpStaffSummaryClient{client: server.Client()}).GetSelfDeploySummary(context.Background(), options, testPlan(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED))
	if err != nil {
		t.Fatalf("GetSelfDeploySummary(): %v", err)
	}
	if response.Summary.ChainStatus != "approved_ready_for_build" {
		t.Fatalf("chain status = %q", response.Summary.ChainStatus)
	}
	if actorType != defaultActorType || actorID != defaultActorID || requestID != "req-1" {
		t.Fatalf("headers actor=%s/%s request=%s", actorType, actorID, requestID)
	}
}

func testOptions() chainOptions {
	return chainOptions{
		ProjectRef:        "project:platform",
		RepositoryRef:     "repository:kodex",
		ProviderSignalRef: "provider:signal:1",
		ActorType:         defaultActorType,
		ActorID:           defaultActorID,
		RequestID:         "req-1",
		StaffGatewayURL:   "http://staff-gateway",
	}
}

func testPlan(status agentsv1.SelfDeployPlanStatus) *agentsv1.SelfDeployPlan {
	return &agentsv1.SelfDeployPlan{
		Id:                  "plan-1",
		ProjectRef:          "project:platform",
		RepositoryRef:       "repository:kodex",
		ProviderSignalRef:   stringPtr("provider:signal:1"),
		SourceRef:           "refs/heads/main",
		MergeCommitSha:      "0123456789abcdef0123456789abcdef01234567",
		ServicesYamlRef:     stringPtr("project-catalog:services-policy/1"),
		ServicesYamlDigest:  "sha256:services",
		AffectedServiceKeys: []string{"agent-manager"},
		ExpectedRuntimeJobTypes: []runtimev1.JobType{
			runtimev1.JobType_JOB_TYPE_BUILD,
			runtimev1.JobType_JOB_TYPE_DEPLOY,
		},
		GovernanceContext: &agentsv1.GovernanceContextRef{
			GateRequestRef: stringPtr("gate-1"),
			GatePolicyRef:  stringPtr("gate-policy:self-deploy"),
		},
		SafeSummary:     stringPtr("safe self-deploy plan"),
		PlanFingerprint: "sha256:plan",
		Status:          status,
		Version:         5,
	}
}

func stageByName(report chainReport, name string) chainStage {
	for _, stage := range report.Stages {
		if stage.Name == name {
			return stage
		}
	}
	return chainStage{}
}

func stringPtr(value string) *string {
	return &value
}

type fakeAgentManager struct {
	getResponse  *agentsv1.SelfDeployPlanResponse
	listResponse *agentsv1.ListSelfDeployPlansResponse
}

func (fake *fakeAgentManager) GetSelfDeployPlan(context.Context, *agentsv1.GetSelfDeployPlanRequest, ...grpc.CallOption) (*agentsv1.SelfDeployPlanResponse, error) {
	return fake.getResponse, nil
}

func (fake *fakeAgentManager) ListSelfDeployPlans(context.Context, *agentsv1.ListSelfDeployPlansRequest, ...grpc.CallOption) (*agentsv1.ListSelfDeployPlansResponse, error) {
	return fake.listResponse, nil
}

type fakeProjectCatalog struct {
	response *projectsv1.SelfDeploySignalResponse
}

func (fake *fakeProjectCatalog) GetSelfDeploySignal(context.Context, *projectsv1.GetSelfDeploySignalRequest, ...grpc.CallOption) (*projectsv1.SelfDeploySignalResponse, error) {
	return fake.response, nil
}

type fakeGovernanceManager struct {
	response *governancev1.GovernanceSummaryResponse
}

func (fake *fakeGovernanceManager) GetGovernanceSummary(context.Context, *governancev1.GetGovernanceSummaryRequest, ...grpc.CallOption) (*governancev1.GovernanceSummaryResponse, error) {
	return fake.response, nil
}

type fakeRuntimeManager struct {
	buildContextResponse *runtimev1.BuildContextResponse
	jobResponses         map[string]*runtimev1.JobResponse
}

func (fake *fakeRuntimeManager) GetBuildContext(context.Context, *runtimev1.GetBuildContextRequest, ...grpc.CallOption) (*runtimev1.BuildContextResponse, error) {
	return fake.buildContextResponse, nil
}

func (fake *fakeRuntimeManager) GetJob(_ context.Context, request *runtimev1.GetJobRequest, _ ...grpc.CallOption) (*runtimev1.JobResponse, error) {
	if fake.jobResponses == nil {
		return &runtimev1.JobResponse{}, nil
	}
	return fake.jobResponses[request.GetJobId()], nil
}

type fakeStaffGateway struct {
	response *staffSummaryResponse
}

func (fake *fakeStaffGateway) GetSelfDeploySummary(context.Context, chainOptions, *agentsv1.SelfDeployPlan) (*staffSummaryResponse, error) {
	return fake.response, nil
}
