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
	"google.golang.org/grpc"
)

type selfDeployDeployPlanClient interface {
	GetSelfDeployDeployPlan(context.Context, *projectsv1.GetSelfDeployDeployPlanRequest, ...grpc.CallOption) (*projectsv1.SelfDeployDeployPlanResponse, error)
}

// SelfDeployDeployPlanReader reads checked project-owned deploy plans from project-catalog.
type SelfDeployDeployPlanReader struct {
	client    selfDeployDeployPlanClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.SelfDeployDeployPlanReader = (*SelfDeployDeployPlanReader)(nil)

// NewSelfDeployDeployPlanReader creates a project-catalog self-deploy deploy plan reader.
func NewSelfDeployDeployPlanReader(client projectsv1.ProjectCatalogServiceClient, cfg Config) (*SelfDeployDeployPlanReader, error) {
	return newSelfDeployDeployPlanReader(client, cfg)
}

func newSelfDeployDeployPlanReader(client selfDeployDeployPlanClient, cfg Config) (*SelfDeployDeployPlanReader, error) {
	return buildProjectCatalogAdapter(client, cfg, &SelfDeployDeployPlanReader{client: client})
}

func (r *SelfDeployDeployPlanReader) applyProjectCatalogSettings(settings grpcclient.ClientSettings) {
	r.authToken = settings.AuthToken
	r.timeout = settings.Timeout
}

// GetSelfDeployDeployPlan returns checked runtime deploy specs for successful self-deploy builds.
func (r *SelfDeployDeployPlanReader) GetSelfDeployDeployPlan(ctx context.Context, input agentservice.SelfDeployDeployPlanLookupInput) (result agentservice.SelfDeployDeployPlanReadResult, err error) {
	if r == nil {
		err = errs.ErrDependencyUnavailable
		return result, err
	}
	response, readErr := readSelfDeployProjectPlan(ctx, r.authToken, r.timeout, r.client, input.ProjectID, input.RepositoryID, func(callCtx context.Context) (*projectsv1.SelfDeployDeployPlanResponse, error) {
		return r.client.GetSelfDeployDeployPlan(callCtx, selfDeployDeployPlanRequest(input))
	})
	if readErr != nil {
		err = readErr
		return result, err
	}
	return selfDeployDeployPlanReadResult(response)
}

func selfDeployDeployPlanRequest(input agentservice.SelfDeployDeployPlanLookupInput) *projectsv1.GetSelfDeployDeployPlanRequest {
	return &projectsv1.GetSelfDeployDeployPlanRequest{
		ProjectId:                         input.ProjectID.String(),
		RepositoryId:                      input.RepositoryID.String(),
		SourceRef:                         strings.TrimSpace(input.SourceRef),
		MergeCommitSha:                    strings.TrimSpace(input.MergeCommitSHA),
		ProviderSignalRef:                 optionalString(input.ProviderSignalRef),
		AffectedServiceKeys:               trimmedStrings(input.AffectedServiceKeys),
		ExpectedServicesPolicyDigest:      strings.TrimSpace(input.ExpectedServicesPolicyDigest),
		ExpectedServicesPolicyFingerprint: optionalString(input.ExpectedServicesPolicyFingerprint),
		ExpectedServicesPolicyVersion:     input.ExpectedServicesPolicyVersion,
		ExpectedBuildPlanFingerprint:      optionalString(input.ExpectedBuildPlanFingerprint),
		ExpectedDeployPlanFingerprint:     optionalString(input.ExpectedDeployPlanFingerprint),
		BuildOutputs:                      selfDeployBuildOutputs(input.BuildOutputs),
		MaterializedBuildContexts:         selfDeployMaterializedBuildContexts(input.MaterializedBuildContexts),
		Meta:                              queryMeta(input.Meta),
	}
}

func selfDeployBuildOutputs(values []agentservice.SelfDeployBuildOutput) []*projectsv1.SelfDeployBuildOutput {
	result := make([]*projectsv1.SelfDeployBuildOutput, 0, len(values))
	for _, value := range values {
		result = append(result, &projectsv1.SelfDeployBuildOutput{
			ServiceKey:               strings.TrimSpace(value.ServiceKey),
			RuntimeJobRef:            optionalString(strings.TrimSpace(value.RuntimeJobRef)),
			ImageRef:                 strings.TrimSpace(value.ImageRef),
			ImageTag:                 strings.TrimSpace(value.ImageTag),
			ImageDigest:              optionalString(strings.TrimSpace(value.ImageDigest)),
			BuildPlanItemFingerprint: optionalString(strings.TrimSpace(value.BuildPlanItemFingerprint)),
			BuildPlanFingerprint:     optionalString(strings.TrimSpace(value.BuildPlanFingerprint)),
			BuildContextRef:          optionalString(strings.TrimSpace(value.BuildContextRef)),
			BuildContextDigest:       optionalString(strings.TrimSpace(value.BuildContextDigest)),
		})
	}
	return result
}

func selfDeployDeployPlanReadResult(response *projectsv1.SelfDeployDeployPlanResponse) (result agentservice.SelfDeployDeployPlanReadResult, err error) {
	if response == nil {
		err = errs.ErrDependencyUnavailable
		return result, err
	}
	status := selfDeployDeployPlanStatus(response.GetStatus())
	reason := strings.TrimSpace(response.GetSafeReason())
	rawPlan := response.GetPlan()
	if rawPlan == nil {
		return agentservice.SelfDeployDeployPlanReadResult{Status: status, SafeReason: reason}, nil
	}
	plan, planErr := selfDeployDeployPlan(rawPlan)
	if planErr != nil {
		err = planErr
		return result, err
	}
	result.Status = status
	result.SafeReason = reason
	result.Plan = plan
	return result, nil
}

func selfDeployDeployPlan(plan *projectsv1.SelfDeployDeployPlan) (agentservice.SelfDeployDeployPlan, error) {
	items, err := selfDeployDeployPlanItems(plan.GetDeployItems())
	if err != nil {
		return agentservice.SelfDeployDeployPlan{}, err
	}
	header := selfDeployProjectPlanHeader(
		plan.GetProjectRef(),
		plan.GetRepositoryRef(),
		plan.GetProviderSignalRef(),
		plan.GetSourceRef(),
		plan.GetMergeCommitSha(),
		plan.GetServicesYaml(),
		plan.GetAffectedServiceKeys(),
		plan.GetPlanFingerprint(),
		plan.GetSafeSummary(),
		plan.GetVersion(),
	)
	converted := agentservice.SelfDeployDeployPlan{}
	converted.ProjectRef = header.projectRef
	converted.RepositoryRef = header.repositoryRef
	converted.ProviderSignalRef = header.providerSignalRef
	converted.SourceRef = header.sourceRef
	converted.MergeCommitSHA = header.mergeCommitSHA
	converted.ServicesYAML = header.servicesYAML
	converted.AffectedServiceKeys = header.affectedServiceKeys
	converted.DeployItems = items
	converted.PlanFingerprint = header.planFingerprint
	converted.SafeSummary = header.safeSummary
	converted.Version = header.version
	return converted, nil
}

func selfDeployDeployPlanItems(items []*projectsv1.SelfDeployDeployPlanItem) ([]agentservice.SelfDeployDeployPlanItem, error) {
	result := make([]agentservice.SelfDeployDeployPlanItem, len(items))
	count := 0
	for _, item := range items {
		if item == nil {
			continue
		}
		spec := agentservice.SelfDeployDeployExecutionSpec{}
		if item.GetDeployExecutionSpec() != nil {
			converted, err := selfDeployDeployExecutionSpec(item.GetDeployExecutionSpec())
			if err != nil {
				return nil, err
			}
			spec = converted
		}
		result[count] = agentservice.SelfDeployDeployPlanItem{
			ServiceKey:          strings.TrimSpace(item.GetServiceKey()),
			ServiceRef:          strings.TrimSpace(item.GetServiceRef()),
			Status:              selfDeployDeployPlanItemStatus(item.GetStatus()),
			DeployExecutionSpec: spec,
			PlanItemFingerprint: strings.TrimSpace(item.GetPlanItemFingerprint()),
			SafeReason:          strings.TrimSpace(item.GetSafeReason()),
		}
		count++
	}
	return result[:count], nil
}

func selfDeployDeployExecutionSpec(spec *runtimev1.DeployExecutionSpec) (agentservice.SelfDeployDeployExecutionSpec, error) {
	if spec == nil {
		return agentservice.SelfDeployDeployExecutionSpec{}, errs.ErrDependencyUnavailable
	}
	return agentservice.SelfDeployDeployExecutionSpec{
		SelfDeployDeploySourceSpec: agentservice.SelfDeployDeploySourceSpec{
			SourceRef:       strings.TrimSpace(spec.GetSourceRef()),
			SourceCommitSHA: strings.TrimSpace(spec.GetSourceCommitSha()),
		},
		SelfDeployDeployImageSpec: agentservice.SelfDeployDeployImageSpec{
			ImageRef:          strings.TrimSpace(spec.GetImageRef()),
			ImageTag:          strings.TrimSpace(spec.GetImageTag()),
			ImageDigest:       strings.TrimSpace(spec.GetImageDigest()),
			ExpectedImageRefs: selfDeployDeployExpectedImageRefs(spec.GetExpectedImageRefs()),
		},
		SelfDeployDeployManifestSpec: agentservice.SelfDeployDeployManifestSpec{
			ManifestRef:          strings.TrimSpace(spec.GetManifestRef()),
			ManifestDigest:       strings.TrimSpace(spec.GetManifestDigest()),
			KustomizationRef:     strings.TrimSpace(spec.GetKustomizationRef()),
			KustomizationDigest:  strings.TrimSpace(spec.GetKustomizationDigest()),
			ManifestBundleRef:    strings.TrimSpace(spec.GetManifestBundleRef()),
			ManifestBundleDigest: strings.TrimSpace(spec.GetManifestBundleDigest()),
		},
		SelfDeployDeployTargetSpec: agentservice.SelfDeployDeployTargetSpec{
			TargetNamespace:  strings.TrimSpace(spec.GetTargetNamespace()),
			TargetClusterRef: strings.TrimSpace(spec.GetTargetClusterRef()),
			TargetSlotID:     strings.TrimSpace(spec.GetTargetSlotId()),
			RolloutTargets:   selfDeployDeployRolloutTargets(spec.GetRolloutTargets()),
		},
		ServiceKey:            strings.TrimSpace(spec.GetServiceKey()),
		DeployPlanFingerprint: strings.TrimSpace(spec.GetDeployPlanFingerprint()),
		AllowedSecretRefs:     selfDeployAllowedSecretRefs(spec.GetAllowedSecretRefs()),
		OutputRefs:            selfDeployOutputRefs(spec.GetOutputRefs()),
	}, nil
}

func selfDeployDeployRolloutTargets(targets []*runtimev1.DeployRolloutTarget) []agentservice.SelfDeployDeployRolloutTarget {
	result := make([]agentservice.SelfDeployDeployRolloutTarget, len(targets))
	count := 0
	for _, target := range targets {
		if target == nil {
			continue
		}
		result[count] = agentservice.SelfDeployDeployRolloutTarget{
			Kind:      strings.TrimSpace(target.GetKind()),
			Ref:       strings.TrimSpace(target.GetRef()),
			Namespace: strings.TrimSpace(target.GetNamespace()),
			Name:      strings.TrimSpace(target.GetName()),
			Digest:    strings.TrimSpace(target.GetDigest()),
		}
		count++
	}
	return result[:count]
}

func selfDeployDeployExpectedImageRefs(refs []*runtimev1.DeployExpectedImageRef) []agentservice.SelfDeployDeployExpectedImageRef {
	result := make([]agentservice.SelfDeployDeployExpectedImageRef, len(refs))
	count := 0
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		result[count] = agentservice.SelfDeployDeployExpectedImageRef{
			ContainerName: strings.TrimSpace(ref.GetContainerName()),
			ImageRef:      strings.TrimSpace(ref.GetImageRef()),
			ImageDigest:   strings.TrimSpace(ref.GetImageDigest()),
		}
		count++
	}
	return result[:count]
}

func selfDeployDeployPlanStatus(status projectsv1.SelfDeployDeployPlanStatus) agentservice.SelfDeployDeployPlanStatus {
	switch status {
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_READY:
		return agentservice.SelfDeployDeployPlanStatusReady
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_DEPLOY_PLAN_UNAVAILABLE:
		return agentservice.SelfDeployDeployPlanStatusDeployPlanUnavailable
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_POLICY_STALE:
		return agentservice.SelfDeployDeployPlanStatusPolicyStale
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_SERVICE_NOT_FOUND:
		return agentservice.SelfDeployDeployPlanStatusServiceNotFound
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_INVALID_INPUT:
		return agentservice.SelfDeployDeployPlanStatusInvalidInput
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_BUILD_NOT_READY:
		return agentservice.SelfDeployDeployPlanStatusBuildNotReady
	case projectsv1.SelfDeployDeployPlanStatus_SELF_DEPLOY_DEPLOY_PLAN_STATUS_BUILD_OUTPUT_INVALID:
		return agentservice.SelfDeployDeployPlanStatusBuildOutputInvalid
	default:
		return ""
	}
}

func selfDeployDeployPlanItemStatus(status projectsv1.SelfDeployDeployPlanItemStatus) agentservice.SelfDeployDeployPlanItemStatus {
	statuses := map[projectsv1.SelfDeployDeployPlanItemStatus]agentservice.SelfDeployDeployPlanItemStatus{
		projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_READY:                   agentservice.SelfDeployDeployPlanItemStatusReady,
		projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_BUILD_NOT_READY:         agentservice.SelfDeployDeployPlanItemStatusBuildNotReady,
		projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_BUILD_OUTPUT_INVALID:    agentservice.SelfDeployDeployPlanItemStatusBuildOutputInvalid,
		projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_DEPLOY_PLAN_UNAVAILABLE: agentservice.SelfDeployDeployPlanItemStatusDeployPlanUnavailable,
	}
	return statuses[status]
}
