package projectcatalog

import (
	"context"
	"strings"
	"time"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/clients/grpcclient"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type selfDeploySignalClient interface {
	GetSelfDeploySignal(context.Context, *projectsv1.GetSelfDeploySignalRequest, ...grpc.CallOption) (*projectsv1.SelfDeploySignalResponse, error)
}

// SelfDeploySignalReader reads project-side self-deploy signal enrichment from project-catalog.
type SelfDeploySignalReader struct {
	client    selfDeploySignalClient
	authToken string
	timeout   time.Duration
}

var _ agentservice.SelfDeploySignalReader = (*SelfDeploySignalReader)(nil)

// NewSelfDeploySignalReader creates a project-catalog self-deploy signal reader.
func NewSelfDeploySignalReader(client projectsv1.ProjectCatalogServiceClient, cfg Config) (*SelfDeploySignalReader, error) {
	return newSelfDeploySignalReader(client, cfg)
}

func newSelfDeploySignalReader(client selfDeploySignalClient, cfg Config) (*SelfDeploySignalReader, error) {
	return buildProjectCatalogAdapter(client, cfg, &SelfDeploySignalReader{client: client})
}

func (r *SelfDeploySignalReader) applyProjectCatalogSettings(settings grpcclient.ClientSettings) {
	r.authToken = settings.AuthToken
	r.timeout = settings.Timeout
}

// GetSelfDeploySignal returns checked project-side input for one provider repository change signal.
func (r *SelfDeploySignalReader) GetSelfDeploySignal(ctx context.Context, input agentservice.SelfDeploySignalLookupInput) (agentservice.SelfDeploySignalReadResult, error) {
	if r == nil || r.client == nil {
		return agentservice.SelfDeploySignalReadResult{}, errs.ErrDependencyUnavailable
	}
	if input.ProjectID == uuid.Nil {
		return agentservice.SelfDeploySignalReadResult{}, errs.ErrInvalidArgument
	}
	callCtx, cancel := context.WithTimeout(grpcclient.OutgoingContext(ctx, r.authToken, callerID), r.timeout)
	defer cancel()
	response, err := r.client.GetSelfDeploySignal(callCtx, &projectsv1.GetSelfDeploySignalRequest{
		ProjectId:         input.ProjectID.String(),
		RepositoryId:      optionalUUIDString(input.RepositoryID),
		ProviderSignalId:  optionalString(strings.TrimSpace(input.ProviderSignalID)),
		ProviderSignalKey: optionalString(strings.TrimSpace(input.ProviderSignalKey)),
		Meta:              queryMeta(input.Meta),
	})
	if err != nil {
		return agentservice.SelfDeploySignalReadResult{}, mapProjectCatalogError(err)
	}
	return selfDeploySignalReadResult(response)
}

func selfDeploySignalReadResult(response *projectsv1.SelfDeploySignalResponse) (agentservice.SelfDeploySignalReadResult, error) {
	if response == nil {
		return agentservice.SelfDeploySignalReadResult{}, errs.ErrDependencyUnavailable
	}
	result := agentservice.SelfDeploySignalReadResult{
		Status:     selfDeploySignalStatus(response.GetStatus()),
		SafeReason: strings.TrimSpace(response.GetSafeReason()),
	}
	if response.GetSignal() != nil {
		signal, err := selfDeploySignal(response.GetSignal())
		if err != nil {
			return agentservice.SelfDeploySignalReadResult{}, err
		}
		result.Signal = signal
	}
	return result, nil
}

func selfDeploySignal(signal *projectsv1.SelfDeploySignal) (agentservice.SelfDeploySignal, error) {
	categories, err := selfDeployPathCategories(signal.GetPathCategories())
	if err != nil {
		return agentservice.SelfDeploySignal{}, err
	}
	jobTypes, err := selfDeployRuntimeJobTypes(signal.GetExpectedRuntimeJobTypes())
	if err != nil {
		return agentservice.SelfDeploySignal{}, err
	}
	return agentservice.SelfDeploySignal{
		ProviderSignalRef:         strings.TrimSpace(signal.GetProviderSignalRef()),
		ProviderSignalID:          strings.TrimSpace(signal.GetProviderSignalId()),
		ProviderSignalKey:         strings.TrimSpace(signal.GetProviderSignalKey()),
		ProjectRef:                strings.TrimSpace(signal.GetProjectRef()),
		RepositoryRef:             strings.TrimSpace(signal.GetRepositoryRef()),
		SourceRef:                 strings.TrimSpace(signal.GetSourceRef()),
		MergeCommitSHA:            strings.TrimSpace(signal.GetMergeCommitSha()),
		ServicesYAML:              selfDeployServicesYAML(signal.GetServicesYaml()),
		AffectedServiceKeys:       trimmedStrings(signal.GetAffectedServiceKeys()),
		PathCategories:            categories,
		ExpectedRuntimeJobTypes:   jobTypes,
		GovernanceRequirement:     selfDeployGovernanceRequirement(signal.GetGovernanceRequirement()),
		ProviderChangeFingerprint: strings.TrimSpace(signal.GetProviderChangeFingerprint()),
		ProjectSignalFingerprint:  strings.TrimSpace(signal.GetProjectSignalFingerprint()),
		SafeSummary:               strings.TrimSpace(signal.GetSafeSummary()),
		Version:                   signal.GetVersion(),
	}, nil
}

var selfDeploySignalStatusByProto = map[projectsv1.SelfDeploySignalStatus]agentservice.SelfDeploySignalStatus{
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY:                           agentservice.SelfDeploySignalStatusReady,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NEEDS_SERVICES_POLICY_RECONCILE: agentservice.SelfDeploySignalStatusNeedsServicesPolicyReconcile,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_PROVIDER_SIGNAL_NOT_FOUND:       agentservice.SelfDeploySignalStatusProviderSignalNotFound,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_PROVIDER_SIGNAL_NOT_READY:       agentservice.SelfDeploySignalStatusProviderSignalNotReady,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_REPOSITORY_BINDING_NOT_FOUND:    agentservice.SelfDeploySignalStatusRepositoryBindingNotFound,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_SERVICES_POLICY_NOT_FOUND:       agentservice.SelfDeploySignalStatusServicesPolicyNotFound,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_SERVICES_POLICY_NOT_READY:       agentservice.SelfDeploySignalStatusServicesPolicyNotReady,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NOT_DEPLOY_RELEVANT:             agentservice.SelfDeploySignalStatusNotDeployRelevant,
	projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NEEDS_REPOSITORY_CHANGE_SUMMARY: agentservice.SelfDeploySignalStatusNeedsRepositoryChangeSummary,
}

func selfDeploySignalStatus(status projectsv1.SelfDeploySignalStatus) agentservice.SelfDeploySignalStatus {
	return selfDeploySignalStatusByProto[status]
}

func selfDeployServicesYAML(policy *projectsv1.SelfDeployServicesYamlProjection) agentservice.SelfDeploySignalServicesYAML {
	if policy == nil {
		return agentservice.SelfDeploySignalServicesYAML{}
	}
	return agentservice.SelfDeploySignalServicesYAML{
		Ref:         strings.TrimSpace(policy.GetServicesYamlRef()),
		Digest:      strings.TrimSpace(policy.GetServicesYamlDigest()),
		Fingerprint: strings.TrimSpace(policy.GetServicesYamlFingerprint()),
		Version:     policy.GetPolicyVersion(),
	}
}

func selfDeployPathCategories(categories []*projectsv1.SelfDeployPathCategoryCount) ([]enum.SelfDeployPathCategory, error) {
	result := make([]enum.SelfDeployPathCategory, 0, len(categories))
	for _, category := range categories {
		if category == nil || category.GetCount() <= 0 {
			continue
		}
		mapped, ok := selfDeployPathCategory(category.GetCategory())
		if !ok {
			return nil, errs.ErrDependencyUnavailable
		}
		result = append(result, mapped)
	}
	return result, nil
}

func selfDeployPathCategory(category projectsv1.SelfDeployPathCategory) (enum.SelfDeployPathCategory, bool) {
	switch category {
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICES_POLICY:
		return enum.SelfDeployPathCategoryServicesPolicy, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICE_SOURCE:
		return enum.SelfDeployPathCategoryServiceSource, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICE_CONFIG:
		return enum.SelfDeployPathCategoryServiceConfig, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_DEPLOY_MANIFEST:
		return enum.SelfDeployPathCategoryDeployManifest, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_RUNTIME_CONFIG:
		return enum.SelfDeployPathCategoryRuntimeConfig, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_DOCUMENTATION:
		return enum.SelfDeployPathCategoryDocumentation, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_TEST:
		return enum.SelfDeployPathCategoryTest, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_PLATFORM_POLICY:
		return enum.SelfDeployPathCategoryPlatformPolicy, true
	case projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_OTHER:
		return enum.SelfDeployPathCategoryOther, true
	default:
		return "", false
	}
}

func selfDeployRuntimeJobTypes(jobTypes []projectsv1.SelfDeployExpectedRuntimeJobType) ([]enum.SelfDeployRuntimeJobType, error) {
	result := make([]enum.SelfDeployRuntimeJobType, 0, len(jobTypes))
	for _, jobType := range jobTypes {
		mapped, ok := selfDeployRuntimeJobType(jobType)
		if !ok {
			return nil, errs.ErrDependencyUnavailable
		}
		result = append(result, mapped)
	}
	return result, nil
}

func selfDeployRuntimeJobType(jobType projectsv1.SelfDeployExpectedRuntimeJobType) (enum.SelfDeployRuntimeJobType, bool) {
	switch jobType {
	case projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_BUILD:
		return enum.SelfDeployRuntimeJobTypeBuild, true
	case projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_DEPLOY:
		return enum.SelfDeployRuntimeJobTypeDeploy, true
	case projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_HEALTH_CHECK:
		return enum.SelfDeployRuntimeJobTypeHealthCheck, true
	default:
		return "", false
	}
}

func selfDeployGovernanceRequirement(requirement *projectsv1.SelfDeployGovernanceRequirement) agentservice.SelfDeployGovernanceRequirement {
	if requirement == nil {
		return agentservice.SelfDeployGovernanceRequirement{}
	}
	return agentservice.SelfDeployGovernanceRequirement{
		GateRequired:   requirement.GetGateRequired(),
		RiskProfileRef: strings.TrimSpace(requirement.GetRiskProfileRef()),
		GatePolicyRef:  strings.TrimSpace(requirement.GetGatePolicyRef()),
	}
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil || *id == uuid.Nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
