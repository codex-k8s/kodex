package projectcatalog

import (
	"context"
	"testing"
	"time"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

func TestSelfDeploySignalReaderMapsReadySignal(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	repositoryID := uuid.MustParse("22222222-3333-4444-8555-666666666666")
	client := &fakeSelfDeploySignalClient{
		response: &projectsv1.SelfDeploySignalResponse{
			Status: projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY,
			Signal: &projectsv1.SelfDeploySignal{
				ProviderSignalRef: "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
				ProviderSignalId:  stringPtr("33333333-4444-4555-8666-777777777777"),
				ProviderSignalKey: stringPtr("provider:github:repository_change:push:codex-k8s/kodex:main:abc123"),
				ProjectRef:        projectID.String(),
				RepositoryRef:     repositoryID.String(),
				SourceRef:         "refs/heads/main",
				MergeCommitSha:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				ServicesYaml: &projectsv1.SelfDeployServicesYamlProjection{
					ServicesYamlRef:         "project-catalog:services-policy:policy-1:services.yaml",
					ServicesYamlDigest:      "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					ServicesYamlFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
					PolicyVersion:           7,
				},
				AffectedServiceKeys: []string{"agent-manager", "runtime-manager"},
				PathCategories: []*projectsv1.SelfDeployPathCategoryCount{
					{Category: projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICES_POLICY, Count: 1},
					{Category: projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_DEPLOY_MANIFEST, Count: 2},
				},
				ExpectedRuntimeJobTypes: []projectsv1.SelfDeployExpectedRuntimeJobType{
					projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_BUILD,
					projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_DEPLOY,
					projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_HEALTH_CHECK,
				},
				GovernanceRequirement: &projectsv1.SelfDeployGovernanceRequirement{
					GateRequired:  true,
					GatePolicyRef: stringPtr("self_deploy.owner_gate"),
				},
				ProviderChangeFingerprint: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				ProjectSignalFingerprint:  "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				SafeSummary:               "self-deploy signal ready",
				Version:                   1,
			},
		},
	}
	reader, err := newSelfDeploySignalReader(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeploySignalReader(): %v", err)
	}

	result, err := reader.GetSelfDeploySignal(context.Background(), agentservice.SelfDeploySignalLookupInput{
		ProjectID:         projectID,
		RepositoryID:      &repositoryID,
		ProviderSignalKey: "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
		Meta:              value.CommandMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != agentservice.SelfDeploySignalStatusReady {
		t.Fatalf("status = %s, want ready", result.Status)
	}
	if client.request.GetProjectId() != projectID.String() || client.request.GetRepositoryId() != repositoryID.String() {
		t.Fatalf("request ids = %q/%q", client.request.GetProjectId(), client.request.GetRepositoryId())
	}
	if result.Signal.ServicesYAML.Digest != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("services yaml digest = %q", result.Signal.ServicesYAML.Digest)
	}
	if len(result.Signal.PathCategories) != 2 || result.Signal.PathCategories[0] != enum.SelfDeployPathCategoryServicesPolicy {
		t.Fatalf("path categories = %v", result.Signal.PathCategories)
	}
	if len(result.Signal.ExpectedRuntimeJobTypes) != 3 || result.Signal.ExpectedRuntimeJobTypes[0] != enum.SelfDeployRuntimeJobTypeBuild {
		t.Fatalf("job types = %v", result.Signal.ExpectedRuntimeJobTypes)
	}
	if result.Signal.GovernanceRequirement.GatePolicyRef != "self_deploy.owner_gate" {
		t.Fatalf("gate policy ref = %q", result.Signal.GovernanceRequirement.GatePolicyRef)
	}
}

func TestSelfDeploySignalReaderMapsNotReadyStatus(t *testing.T) {
	t.Parallel()

	reader, err := newSelfDeploySignalReader(&fakeSelfDeploySignalClient{
		response: &projectsv1.SelfDeploySignalResponse{
			Status:     projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NEEDS_SERVICES_POLICY_RECONCILE,
			SafeReason: stringPtr("services_policy_commit_not_reconciled"),
		},
	}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeploySignalReader(): %v", err)
	}

	result, err := reader.GetSelfDeploySignal(context.Background(), agentservice.SelfDeploySignalLookupInput{
		ProjectID:        uuid.New(),
		ProviderSignalID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeploySignal(): %v", err)
	}
	if result.Status != agentservice.SelfDeploySignalStatusNeedsServicesPolicyReconcile || result.SafeReason != "services_policy_commit_not_reconciled" {
		t.Fatalf("result = %+v", result)
	}
}

type fakeSelfDeploySignalClient struct {
	request  *projectsv1.GetSelfDeploySignalRequest
	response *projectsv1.SelfDeploySignalResponse
	err      error
}

func (f *fakeSelfDeploySignalClient) GetSelfDeploySignal(_ context.Context, request *projectsv1.GetSelfDeploySignalRequest, _ ...grpc.CallOption) (*projectsv1.SelfDeploySignalResponse, error) {
	f.request = request
	return f.response, f.err
}

func stringPtr(value string) *string { return &value }
