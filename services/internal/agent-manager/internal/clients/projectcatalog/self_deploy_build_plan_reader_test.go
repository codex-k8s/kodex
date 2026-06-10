package projectcatalog

import (
	"context"
	"testing"
	"time"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

func TestSelfDeployBuildPlanReaderMapsReadyBuildPlan(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("63135040-fe44-4ec4-83d5-b0126dc23b32")
	repositoryID := uuid.MustParse("63135040-fe44-4ec4-83d5-b0126dc23b33")
	client := &fakeSelfDeployBuildPlanClient{
		response: &projectsv1.SelfDeployBuildPlanResponse{
			Status: projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_READY,
			Plan: &projectsv1.SelfDeployBuildPlan{
				ProjectRef:        projectID.String(),
				RepositoryRef:     repositoryID.String(),
				ProviderSignalRef: stringPtr("provider-signal-ref"),
				SourceRef:         "refs/heads/main",
				MergeCommitSha:    "abcdef0123456789abcdef0123456789abcdef01",
				ServicesYaml: &projectsv1.SelfDeployServicesYamlProjection{
					ServicesYamlRef:         "project-catalog:services-policy:policy-1:services.yaml",
					ServicesYamlDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					ServicesYamlFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					PolicyVersion:           7,
				},
				AffectedServiceKeys: []string{"agent-manager"},
				BuildItems: []*projectsv1.SelfDeployBuildPlanItem{{
					ServiceKey: "agent-manager",
					ServiceRef: "project-catalog:service-descriptor:agent-manager",
					BuildExecutionSpec: &runtimev1.BuildExecutionSpec{
						SourceRef:            "refs/heads/main",
						SourceCommitSha:      "abcdef0123456789abcdef0123456789abcdef01",
						ServiceKey:           "agent-manager",
						ImageRef:             "registry.example/kodex/agent-manager",
						ImageTag:             "abcdef0",
						BuildContextRef:      "runtime://build-contexts/agent-manager",
						BuildContextDigest:   "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
						DockerfileRef:        "runtime://build-contexts/agent-manager/Dockerfile",
						DockerfileTarget:     "prod",
						BuilderImageRef:      "gcr.io/kaniko-project/executor:v1.23.2",
						BuildPlanFingerprint: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
						AllowedSecretRefs: []*runtimev1.RuntimeJobAllowedSecretRef{
							{SecretRef: "secret://runtime/registry", Purpose: "registry_docker_config"},
						},
						OutputRefs: []*runtimev1.RuntimeJobOutputRef{
							{Kind: "image", Ref: "runtime:image:agent-manager"},
						},
					},
					PlanItemFingerprint: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				}},
				PlanFingerprint: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				SafeSummary:     "self-deploy build plan ready",
				Version:         1,
			},
		},
	}
	reader, err := newSelfDeployBuildPlanReader(client, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployBuildPlanReader(): %v", err)
	}

	result, err := reader.GetSelfDeployBuildPlan(context.Background(), agentservice.SelfDeployBuildPlanLookupInput{
		ProjectID:                    projectID,
		RepositoryID:                 repositoryID,
		SourceRef:                    "refs/heads/main",
		MergeCommitSHA:               "abcdef0123456789abcdef0123456789abcdef01",
		ProviderSignalRef:            "provider-signal-ref",
		AffectedServiceKeys:          []string{"agent-manager"},
		ExpectedServicesPolicyDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Meta:                         value.CommandMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != agentservice.SelfDeployBuildPlanStatusReady {
		t.Fatalf("status = %s, want ready", result.Status)
	}
	if client.request.GetExpectedServicesPolicyDigest() != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		len(client.request.GetAffectedServiceKeys()) != 1 {
		t.Fatalf("request = %+v", client.request)
	}
	item := result.Plan.BuildItems[0]
	if item.BuildExecutionSpec.ImageRef != "registry.example/kodex/agent-manager" ||
		item.BuildExecutionSpec.AllowedSecretRefs[0].SecretRef != "secret://runtime/registry" ||
		item.PlanItemFingerprint != "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" {
		t.Fatalf("mapped item = %+v", item)
	}
}

func TestSelfDeployBuildPlanReaderMapsNotReadyStatus(t *testing.T) {
	t.Parallel()

	reader, err := newSelfDeployBuildPlanReader(&fakeSelfDeployBuildPlanClient{
		response: &projectsv1.SelfDeployBuildPlanResponse{
			Status:     projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_BUILD_CONTEXT_UNAVAILABLE,
			SafeReason: stringPtr("build_context_unavailable:agent-manager"),
		},
	}, Config{AuthToken: "token", Timeout: time.Second})
	if err != nil {
		t.Fatalf("newSelfDeployBuildPlanReader(): %v", err)
	}

	result, err := reader.GetSelfDeployBuildPlan(context.Background(), agentservice.SelfDeployBuildPlanLookupInput{
		ProjectID:    uuid.New(),
		RepositoryID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("GetSelfDeployBuildPlan(): %v", err)
	}
	if result.Status != agentservice.SelfDeployBuildPlanStatusBuildContextUnavailable || result.SafeReason != "build_context_unavailable:agent-manager" {
		t.Fatalf("result = %+v", result)
	}
}

type fakeSelfDeployBuildPlanClient struct {
	request  *projectsv1.GetSelfDeployBuildPlanRequest
	response *projectsv1.SelfDeployBuildPlanResponse
	err      error
}

func (f *fakeSelfDeployBuildPlanClient) GetSelfDeployBuildPlan(_ context.Context, request *projectsv1.GetSelfDeployBuildPlanRequest, _ ...grpc.CallOption) (*projectsv1.SelfDeployBuildPlanResponse, error) {
	f.request = request
	return f.response, f.err
}
