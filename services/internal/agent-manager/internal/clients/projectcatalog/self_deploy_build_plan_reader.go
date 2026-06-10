package projectcatalog

import (
	"context"
	"strings"
	"time"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type selfDeployBuildPlanClient interface {
	GetSelfDeployBuildPlan(context.Context, *projectsv1.GetSelfDeployBuildPlanRequest, ...grpc.CallOption) (*projectsv1.SelfDeployBuildPlanResponse, error)
}

// SelfDeployBuildPlanReader reads checked project-owned build plans from project-catalog.
type SelfDeployBuildPlanReader struct {
	client    selfDeployBuildPlanClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.SelfDeployBuildPlanReader = (*SelfDeployBuildPlanReader)(nil)

// NewSelfDeployBuildPlanReader creates a project-catalog self-deploy build plan reader.
func NewSelfDeployBuildPlanReader(client projectsv1.ProjectCatalogServiceClient, cfg Config) (*SelfDeployBuildPlanReader, error) {
	return newSelfDeployBuildPlanReader(client, cfg)
}

func newSelfDeployBuildPlanReader(client selfDeployBuildPlanClient, cfg Config) (*SelfDeployBuildPlanReader, error) {
	return buildProjectCatalogAdapter(client, cfg, &SelfDeployBuildPlanReader{client: client})
}

func (r *SelfDeployBuildPlanReader) applyProjectCatalogSettings(settings grpcclient.ClientSettings) {
	r.authToken = settings.AuthToken
	r.timeout = settings.Timeout
}

// GetSelfDeployBuildPlan returns checked runtime build specs for an approved self-deploy plan.
func (r *SelfDeployBuildPlanReader) GetSelfDeployBuildPlan(ctx context.Context, input agentservice.SelfDeployBuildPlanLookupInput) (agentservice.SelfDeployBuildPlanReadResult, error) {
	if r == nil || r.client == nil {
		return agentservice.SelfDeployBuildPlanReadResult{}, errs.ErrDependencyUnavailable
	}
	if input.ProjectID == uuid.Nil || input.RepositoryID == uuid.Nil {
		return agentservice.SelfDeployBuildPlanReadResult{}, errs.ErrInvalidArgument
	}
	callCtx, cancel := context.WithTimeout(grpcclient.OutgoingContext(ctx, r.authToken, callerID), r.timeout)
	defer cancel()
	response, err := r.client.GetSelfDeployBuildPlan(callCtx, selfDeployBuildPlanRequest(input))
	if err != nil {
		return agentservice.SelfDeployBuildPlanReadResult{}, mapProjectCatalogError(err)
	}
	return selfDeployBuildPlanReadResult(response)
}

func selfDeployBuildPlanRequest(input agentservice.SelfDeployBuildPlanLookupInput) *projectsv1.GetSelfDeployBuildPlanRequest {
	return &projectsv1.GetSelfDeployBuildPlanRequest{
		ProjectId:                         input.ProjectID.String(),
		RepositoryId:                      input.RepositoryID.String(),
		SourceRef:                         strings.TrimSpace(input.SourceRef),
		MergeCommitSha:                    strings.TrimSpace(input.MergeCommitSHA),
		ProviderSignalRef:                 optionalString(input.ProviderSignalRef),
		ProviderSignalId:                  optionalString(input.ProviderSignalID),
		ProviderSignalKey:                 optionalString(input.ProviderSignalKey),
		AffectedServiceKeys:               trimmedStrings(input.AffectedServiceKeys),
		ExpectedServicesPolicyDigest:      strings.TrimSpace(input.ExpectedServicesPolicyDigest),
		ExpectedServicesPolicyFingerprint: optionalString(input.ExpectedServicesPolicyFingerprint),
		ExpectedServicesPolicyVersion:     input.ExpectedServicesPolicyVersion,
		Meta:                              queryMeta(input.Meta),
	}
}

func selfDeployBuildPlanReadResult(response *projectsv1.SelfDeployBuildPlanResponse) (agentservice.SelfDeployBuildPlanReadResult, error) {
	if response == nil {
		return agentservice.SelfDeployBuildPlanReadResult{}, errs.ErrDependencyUnavailable
	}
	status := selfDeployBuildPlanStatus(response.GetStatus())
	reason := strings.TrimSpace(response.GetSafeReason())
	rawPlan := response.GetPlan()
	if rawPlan == nil {
		return agentservice.SelfDeployBuildPlanReadResult{Status: status, SafeReason: reason}, nil
	}
	plan, err := selfDeployBuildPlan(rawPlan)
	if err != nil {
		return agentservice.SelfDeployBuildPlanReadResult{}, err
	}
	return agentservice.SelfDeployBuildPlanReadResult{Status: status, SafeReason: reason, Plan: plan}, nil
}

func selfDeployBuildPlan(plan *projectsv1.SelfDeployBuildPlan) (agentservice.SelfDeployBuildPlan, error) {
	items, err := selfDeployBuildPlanItems(plan.GetBuildItems())
	if err != nil {
		return agentservice.SelfDeployBuildPlan{}, err
	}
	return agentservice.SelfDeployBuildPlan{
		ProjectRef:          strings.TrimSpace(plan.GetProjectRef()),
		RepositoryRef:       strings.TrimSpace(plan.GetRepositoryRef()),
		ProviderSignalRef:   strings.TrimSpace(plan.GetProviderSignalRef()),
		SourceRef:           strings.TrimSpace(plan.GetSourceRef()),
		MergeCommitSHA:      strings.TrimSpace(plan.GetMergeCommitSha()),
		ServicesYAML:        selfDeployServicesYAML(plan.GetServicesYaml()),
		AffectedServiceKeys: trimmedStrings(plan.GetAffectedServiceKeys()),
		BuildItems:          items,
		PlanFingerprint:     strings.TrimSpace(plan.GetPlanFingerprint()),
		SafeSummary:         strings.TrimSpace(plan.GetSafeSummary()),
		Version:             plan.GetVersion(),
	}, nil
}

func selfDeployBuildPlanItems(items []*projectsv1.SelfDeployBuildPlanItem) ([]agentservice.SelfDeployBuildPlanItem, error) {
	result := make([]agentservice.SelfDeployBuildPlanItem, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		spec, err := selfDeployBuildExecutionSpec(item.GetBuildExecutionSpec())
		if err != nil {
			return nil, err
		}
		result = append(result, agentservice.SelfDeployBuildPlanItem{
			ServiceKey:          strings.TrimSpace(item.GetServiceKey()),
			ServiceRef:          strings.TrimSpace(item.GetServiceRef()),
			BuildExecutionSpec:  spec,
			PlanItemFingerprint: strings.TrimSpace(item.GetPlanItemFingerprint()),
		})
	}
	return result, nil
}

func selfDeployBuildExecutionSpec(spec *runtimev1.BuildExecutionSpec) (agentservice.SelfDeployBuildExecutionSpec, error) {
	if spec == nil {
		return agentservice.SelfDeployBuildExecutionSpec{}, errs.ErrDependencyUnavailable
	}
	converted := agentservice.SelfDeployBuildExecutionSpec{}
	converted.ServiceKey = strings.TrimSpace(spec.GetServiceKey())
	converted.SourceRef = strings.TrimSpace(spec.GetSourceRef())
	converted.SourceCommitSHA = strings.TrimSpace(spec.GetSourceCommitSha())
	converted.BuildPlanFingerprint = strings.TrimSpace(spec.GetBuildPlanFingerprint())
	converted.ImageRef = strings.TrimSpace(spec.GetImageRef())
	converted.ImageTag = strings.TrimSpace(spec.GetImageTag())
	converted.ImageDigest = strings.TrimSpace(spec.GetImageDigest())
	converted.BuildContextRef = strings.TrimSpace(spec.GetBuildContextRef())
	converted.BuildContextDigest = strings.TrimSpace(spec.GetBuildContextDigest())
	converted.DockerfileRef = strings.TrimSpace(spec.GetDockerfileRef())
	converted.DockerfileDigest = strings.TrimSpace(spec.GetDockerfileDigest())
	converted.DockerfileTarget = strings.TrimSpace(spec.GetDockerfileTarget())
	converted.BuilderImageRef = strings.TrimSpace(spec.GetBuilderImageRef())
	converted.AllowedSecretRefs = selfDeployAllowedSecretRefs(spec.GetAllowedSecretRefs())
	converted.OutputRefs = selfDeployOutputRefs(spec.GetOutputRefs())
	return converted, nil
}

func selfDeployAllowedSecretRefs(refs []*runtimev1.RuntimeJobAllowedSecretRef) []agentservice.RuntimeJobAllowedSecretRef {
	result := make([]agentservice.RuntimeJobAllowedSecretRef, 0, len(refs))
	for index := range refs {
		ref := refs[index]
		if ref == nil {
			continue
		}
		result = append(result, agentservice.RuntimeJobAllowedSecretRef{
			SecretRef: strings.TrimSpace(ref.GetSecretRef()),
			Purpose:   strings.TrimSpace(ref.GetPurpose()),
		})
	}
	return result
}

func selfDeployOutputRefs(refs []*runtimev1.RuntimeJobOutputRef) []agentservice.RuntimeJobOutputRef {
	result := make([]agentservice.RuntimeJobOutputRef, 0, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		item := agentservice.RuntimeJobOutputRef{
			Kind: strings.TrimSpace(ref.GetKind()),
			Ref:  strings.TrimSpace(ref.GetRef()),
		}
		result = append(result, item)
	}
	return result
}

func selfDeployBuildPlanStatus(status projectsv1.SelfDeployBuildPlanStatus) agentservice.SelfDeployBuildPlanStatus {
	switch status {
	case projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_READY:
		return agentservice.SelfDeployBuildPlanStatusReady
	case projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_BUILD_PLAN_UNAVAILABLE:
		return agentservice.SelfDeployBuildPlanStatusBuildPlanUnavailable
	case projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_POLICY_STALE:
		return agentservice.SelfDeployBuildPlanStatusPolicyStale
	case projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_SERVICE_NOT_FOUND:
		return agentservice.SelfDeployBuildPlanStatusServiceNotFound
	case projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_INVALID_INPUT:
		return agentservice.SelfDeployBuildPlanStatusInvalidInput
	case projectsv1.SelfDeployBuildPlanStatus_SELF_DEPLOY_BUILD_PLAN_STATUS_BUILD_CONTEXT_UNAVAILABLE:
		return agentservice.SelfDeployBuildPlanStatusBuildContextUnavailable
	default:
		return ""
	}
}
