package casters

import (
	"time"

	"github.com/google/uuid"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
)

// ProjectResponse maps a project aggregate to gRPC.
func ProjectResponse(project entity.Project) *projectsv1.ProjectResponse {
	return &projectsv1.ProjectResponse{Project: ProjectToProto(project)}
}

func ProjectToProto(project entity.Project) *projectsv1.Project {
	return &projectsv1.Project{
		ProjectId:      project.ID.String(),
		OrganizationId: project.OrganizationID.String(),
		Slug:           project.Slug,
		DisplayName:    project.DisplayName,
		Description:    stringPtr(project.Description),
		IconObjectUri:  stringPtr(project.IconObjectURI),
		Status:         ProjectStatusToProto(project.Status),
		Version:        project.Version,
	}
}

func ListProjectsResponse(result projectservice.ListProjectsResult) *projectsv1.ListProjectsResponse {
	return &projectsv1.ListProjectsResponse{Projects: mapSlice(result.Projects, ProjectToProto), Page: pageResponseToProto(result.Page)}
}

func RepositoryResponse(repository entity.RepositoryBinding) *projectsv1.RepositoryResponse {
	return &projectsv1.RepositoryResponse{Repository: RepositoryToProto(repository)}
}

func RepositoryToProto(repository entity.RepositoryBinding) *projectsv1.Repository {
	return &projectsv1.Repository{
		RepositoryId:         repository.ID.String(),
		ProjectId:            repository.ProjectID.String(),
		Provider:             RepositoryProviderToProto(repository.Provider),
		ProviderOwner:        repository.ProviderOwner,
		ProviderName:         repository.ProviderName,
		WebUrl:               repository.WebURL,
		DefaultBranch:        repository.DefaultBranch,
		Status:               RepositoryStatusToProto(repository.Status),
		ProviderRepositoryId: stringPtr(repository.ProviderRepositoryID),
		IconObjectUri:        stringPtr(repository.IconObjectURI),
		Version:              repository.Version,
	}
}

func ListRepositoriesResponse(result projectservice.ListRepositoriesResult) *projectsv1.ListRepositoriesResponse {
	return &projectsv1.ListRepositoriesResponse{Repositories: mapSlice(result.Repositories, RepositoryToProto), Page: pageResponseToProto(result.Page)}
}

// RepositoryProviderCreateResponse maps provider repository create result to gRPC.
func RepositoryProviderCreateResponse(result projectservice.RepositoryProviderCreateResult) *projectsv1.RepositoryProviderCreateResponse {
	return &projectsv1.RepositoryProviderCreateResponse{
		Repository:           RepositoryToProto(result.Repository),
		ProviderTarget:       bootstrapProviderTargetToProto(result.ProviderTarget),
		BaseBranch:           result.BaseBranch,
		ProviderOperationId:  optionalStringPtr(result.ProviderResult.ProviderOperationID),
		ProviderResultRef:    optionalStringPtr(result.ProviderResult.ProviderResultRef),
		ProviderRepositoryId: optionalStringPtr(result.ProviderResult.ProviderRepositoryID),
		ProviderWebUrl:       optionalStringPtr(result.ProviderResult.ProviderWebURL),
		ProviderObjectId:     optionalStringPtr(result.ProviderResult.ProviderObjectID),
		ProviderVersion:      optionalStringPtr(result.ProviderResult.ProviderVersion),
	}
}

// RepositoryBootstrapPullRequestResponse maps bootstrap PR result to gRPC.
func RepositoryBootstrapPullRequestResponse(result projectservice.RepositoryBootstrapPullRequestResult) *projectsv1.RepositoryBootstrapPullRequestResponse {
	return &projectsv1.RepositoryBootstrapPullRequestResponse{
		Repository:                   RepositoryToProto(result.Repository),
		ProviderTarget:               bootstrapProviderTargetToProto(result.ProviderTarget),
		BaseBranch:                   result.BaseBranch,
		BootstrapBranch:              result.BootstrapBranch,
		ServicesPolicySourcePath:     result.ServicesPolicy.SourcePath,
		ServicesPolicyContentHash:    result.ServicesPolicy.ContentHash,
		ProviderOperationId:          optionalStringPtr(result.ProviderResult.ProviderOperationID),
		ProviderResultRef:            optionalStringPtr(result.ProviderResult.ProviderResultRef),
		ProviderWorkItemProjectionId: optionalStringPtr(result.ProviderResult.ProviderWorkItemProjectionID),
		ProviderWebUrl:               optionalStringPtr(result.ProviderResult.ProviderWebURL),
		ProviderObjectId:             optionalStringPtr(result.ProviderResult.ProviderObjectID),
	}
}

func bootstrapProviderTargetToProto(target projectservice.RepositoryBootstrapProviderTarget) *projectsv1.RepositoryBootstrapProviderTarget {
	return &projectsv1.RepositoryBootstrapProviderTarget{
		ProviderSlug:         target.ProviderSlug,
		RepositoryFullName:   target.RepositoryFullName,
		ProviderRepositoryId: optionalStringPtr(target.ProviderRepositoryID),
		WebUrl:               optionalStringPtr(target.WebURL),
	}
}

func ServicesPolicyResponse(policy entity.ServicesPolicy) *projectsv1.ServicesPolicyResponse {
	return &projectsv1.ServicesPolicyResponse{ServicesPolicy: ServicesPolicyToProto(policy)}
}

func SelfDeploySignalResponse(result projectservice.SelfDeploySignalResult) *projectsv1.SelfDeploySignalResponse {
	return &projectsv1.SelfDeploySignalResponse{
		Status:     SelfDeploySignalStatusToProto(result.Status),
		Signal:     SelfDeploySignalToProto(result.Signal),
		SafeReason: optionalStringPtr(result.SafeReason),
	}
}

func SelfDeploySignalToProto(signal projectservice.SelfDeploySignal) *projectsv1.SelfDeploySignal {
	if signal.ProviderSignalRef == "" && signal.ProjectRef == "" && signal.RepositoryRef == "" {
		return nil
	}
	return &projectsv1.SelfDeploySignal{
		ProviderSignalRef:         signal.ProviderSignalRef,
		ProviderSignalId:          optionalStringPtr(signal.ProviderSignalID),
		ProviderSignalKey:         optionalStringPtr(signal.ProviderSignalKey),
		ProjectRef:                signal.ProjectRef,
		RepositoryRef:             signal.RepositoryRef,
		ProviderSlug:              signal.ProviderSlug,
		RepositoryFullName:        signal.RepositoryFullName,
		ProviderRepositoryId:      optionalStringPtr(signal.ProviderRepositoryID),
		SourceRef:                 signal.SourceRef,
		MergeCommitSha:            signal.MergeCommitSHA,
		ServicesYaml:              selfDeployServicesYamlToProto(signal.ServicesYaml),
		AffectedServiceKeys:       signal.AffectedServiceKeys,
		PathCategories:            mapSlice(signal.PathCategories, selfDeployPathCategoryCountToProto),
		ServicesYamlChanged:       signal.ServicesYamlChanged,
		DeployRelevantChanged:     signal.DeployRelevantChanged,
		ExpectedRuntimeJobTypes:   selfDeployExpectedRuntimeJobTypesToProto(signal.ExpectedRuntimeJobTypes),
		GovernanceRequirement:     selfDeployGovernanceRequirementToProto(signal.GovernanceRequirement),
		ProviderChangeFingerprint: signal.ProviderChangeFingerprint,
		ProjectSignalFingerprint:  signal.ProjectSignalFingerprint,
		ProviderEtag:              optionalStringPtr(signal.ProviderETag),
		SafeSummary:               signal.SafeSummary,
		ObservedAt:                signal.ObservedAt,
		Version:                   signal.Version,
	}
}

func selfDeployServicesYamlToProto(policy projectservice.SelfDeployServicesYamlProjection) *projectsv1.SelfDeployServicesYamlProjection {
	if policy.ServicesYamlRef == "" && policy.ServicesPolicyID == uuid.Nil {
		return nil
	}
	return &projectsv1.SelfDeployServicesYamlProjection{
		ServicesYamlRef:         policy.ServicesYamlRef,
		ServicesYamlDigest:      policy.ServicesYamlDigest,
		ServicesYamlFingerprint: policy.ServicesYamlFingerprint,
		ServicesPolicyId:        policy.ServicesPolicyID.String(),
		SourceRepositoryId:      uuidPtrString(policy.SourceRepositoryID),
		SourcePath:              policy.SourcePath,
		SourceRef:               optionalStringPtr(policy.SourceRef),
		SourceCommitSha:         policy.SourceCommitSHA,
		PolicyVersion:           policy.PolicyVersion,
		ValidationStatus:        ValidationStatusToProto(policy.ValidationStatus),
		ProjectionStatus:        ProjectionStatusToProto(policy.ProjectionStatus),
		ImportedAt:              policy.ImportedAt,
	}
}

func selfDeployPathCategoryCountToProto(category projectservice.RepositoryChangePathCategoryCount) *projectsv1.SelfDeployPathCategoryCount {
	return &projectsv1.SelfDeployPathCategoryCount{
		Category: SelfDeployPathCategoryToProto(category.Category),
		Count:    category.Count,
	}
}

func selfDeployExpectedRuntimeJobTypesToProto(jobTypes []enum.SelfDeployExpectedRuntimeJobType) []projectsv1.SelfDeployExpectedRuntimeJobType {
	result := make([]projectsv1.SelfDeployExpectedRuntimeJobType, 0, len(jobTypes))
	for _, jobType := range jobTypes {
		result = append(result, SelfDeployExpectedRuntimeJobTypeToProto(jobType))
	}
	return result
}

func selfDeployGovernanceRequirementToProto(requirement projectservice.SelfDeployGovernanceRequirement) *projectsv1.SelfDeployGovernanceRequirement {
	return &projectsv1.SelfDeployGovernanceRequirement{
		GateRequired:   requirement.GateRequired,
		RiskProfileRef: optionalStringPtr(requirement.RiskProfileRef),
		GatePolicyRef:  optionalStringPtr(requirement.GatePolicyRef),
	}
}

func SelfDeployBuildPlanResponse(result projectservice.SelfDeployBuildPlanResult) *projectsv1.SelfDeployBuildPlanResponse {
	return &projectsv1.SelfDeployBuildPlanResponse{
		Status:     SelfDeployBuildPlanStatusToProto(result.Status),
		Plan:       SelfDeployBuildPlanToProto(result.Plan),
		SafeReason: optionalStringPtr(result.SafeReason),
	}
}

func SelfDeployBuildPlanToProto(plan projectservice.SelfDeployBuildPlan) *projectsv1.SelfDeployBuildPlan {
	if plan.ProjectRef == "" && plan.RepositoryRef == "" {
		return nil
	}
	common := selfDeployPlanProtoCommon(plan.ProjectRef, plan.RepositoryRef, plan.ProviderSignalRef, plan.SourceRef, plan.MergeCommitSHA, plan.ServicesYaml, plan.AffectedServiceKeys, plan.PlanFingerprint, plan.SafeSummary, plan.Version)
	return &projectsv1.SelfDeployBuildPlan{
		ProjectRef:          common.projectRef,
		RepositoryRef:       common.repositoryRef,
		ProviderSignalRef:   common.providerSignalRef,
		SourceRef:           common.sourceRef,
		MergeCommitSha:      common.mergeCommitSHA,
		ServicesYaml:        common.servicesYAML,
		AffectedServiceKeys: common.affectedServiceKeys,
		BuildItems:          mapSlice(plan.BuildItems, selfDeployBuildPlanItemToProto),
		PlanFingerprint:     common.planFingerprint,
		SafeSummary:         common.safeSummary,
		Version:             common.version,
	}
}

func selfDeployBuildPlanItemToProto(item projectservice.SelfDeployBuildPlanItem) *projectsv1.SelfDeployBuildPlanItem {
	return &projectsv1.SelfDeployBuildPlanItem{
		ServiceKey:          item.ServiceKey,
		ServiceRef:          item.ServiceRef,
		BuildExecutionSpec:  buildExecutionSpecToProto(item.BuildExecutionSpec),
		PlanItemFingerprint: item.PlanItemFingerprint,
		Status:              selfDeployBuildPlanItemStatusToProto(item.Status),
		BuildRecipe:         selfDeployBuildRecipeToProto(item.BuildRecipe),
		SafeReason:          optionalStringPtr(item.SafeReason),
	}
}

func selfDeployBuildPlanItemStatusToProto(status projectservice.SelfDeployBuildPlanItemStatus) projectsv1.SelfDeployBuildPlanItemStatus {
	statuses := map[projectservice.SelfDeployBuildPlanItemStatus]projectsv1.SelfDeployBuildPlanItemStatus{
		projectservice.SelfDeployBuildPlanItemStatusReady:                projectsv1.SelfDeployBuildPlanItemStatus_SELF_DEPLOY_BUILD_PLAN_ITEM_STATUS_READY,
		projectservice.SelfDeployBuildPlanItemStatusBuildContextRequired: projectsv1.SelfDeployBuildPlanItemStatus_SELF_DEPLOY_BUILD_PLAN_ITEM_STATUS_BUILD_CONTEXT_REQUIRED,
		projectservice.SelfDeployBuildPlanItemStatusBuildContextInvalid:  projectsv1.SelfDeployBuildPlanItemStatus_SELF_DEPLOY_BUILD_PLAN_ITEM_STATUS_BUILD_CONTEXT_INVALID,
		projectservice.SelfDeployBuildPlanItemStatusBuildPlanUnavailable: projectsv1.SelfDeployBuildPlanItemStatus_SELF_DEPLOY_BUILD_PLAN_ITEM_STATUS_BUILD_PLAN_UNAVAILABLE,
	}
	if protoStatus, ok := statuses[status]; ok {
		return protoStatus
	}
	return projectsv1.SelfDeployBuildPlanItemStatus_SELF_DEPLOY_BUILD_PLAN_ITEM_STATUS_UNSPECIFIED
}

func selfDeployBuildRecipeToProto(recipe projectservice.SelfDeployBuildRecipe) *projectsv1.SelfDeployBuildRecipe {
	if recipe.ImageRef == "" && recipe.DockerfileRef == "" {
		return nil
	}
	return &projectsv1.SelfDeployBuildRecipe{
		ImageRef:          recipe.ImageRef,
		ImageTag:          recipe.ImageTag,
		ImageDigest:       optionalStringPtr(recipe.ImageDigest),
		DockerfileRef:     recipe.DockerfileRef,
		DockerfileTarget:  recipe.DockerfileTarget,
		BuilderImageRef:   recipe.BuilderImageRef,
		AllowedSecretRefs: mapSlice(recipe.AllowedSecretRefs, allowedSecretRefToProto),
		OutputRefs:        mapSlice(recipe.OutputRefs, outputRefToProto),
		RecipeFingerprint: recipe.RecipeFingerprint,
	}
}

func buildExecutionSpecToProto(spec projectservice.SelfDeployBuildExecutionSpec) *runtimev1.BuildExecutionSpec {
	if spec.ServiceKey == "" && spec.BuildContextRef == "" {
		return nil
	}
	return &runtimev1.BuildExecutionSpec{
		SourceRef:            spec.SourceRef,
		SourceCommitSha:      spec.SourceCommitSHA,
		ServiceKey:           spec.ServiceKey,
		ImageRef:             spec.ImageRef,
		ImageTag:             spec.ImageTag,
		ImageDigest:          optionalStringPtr(spec.ImageDigest),
		BuildContextRef:      spec.BuildContextRef,
		BuildContextDigest:   spec.BuildContextDigest,
		DockerfileRef:        spec.DockerfileRef,
		DockerfileDigest:     optionalStringPtr(spec.DockerfileDigest),
		DockerfileTarget:     spec.DockerfileTarget,
		BuilderImageRef:      spec.BuilderImageRef,
		BuildPlanFingerprint: spec.BuildPlanFingerprint,
		AllowedSecretRefs:    mapSlice(spec.AllowedSecretRefs, allowedSecretRefToProto),
		OutputRefs:           mapSlice(spec.OutputRefs, outputRefToProto),
	}
}

func allowedSecretRefToProto(ref projectservice.RuntimeJobAllowedSecretRef) *runtimev1.RuntimeJobAllowedSecretRef {
	return &runtimev1.RuntimeJobAllowedSecretRef{SecretRef: ref.SecretRef, Purpose: ref.Purpose}
}

func outputRefToProto(ref projectservice.RuntimeJobOutputRef) *runtimev1.RuntimeJobOutputRef {
	return &runtimev1.RuntimeJobOutputRef{Kind: ref.Kind, Ref: ref.Ref}
}

func SelfDeployDeployPlanResponse(result projectservice.SelfDeployDeployPlanResult) *projectsv1.SelfDeployDeployPlanResponse {
	return &projectsv1.SelfDeployDeployPlanResponse{
		Status:     SelfDeployDeployPlanStatusToProto(result.Status),
		Plan:       SelfDeployDeployPlanToProto(result.Plan),
		SafeReason: optionalStringPtr(result.SafeReason),
	}
}

func SelfDeployDeployPlanToProto(plan projectservice.SelfDeployDeployPlan) *projectsv1.SelfDeployDeployPlan {
	if plan.ProjectRef == "" && plan.RepositoryRef == "" {
		return nil
	}
	common := selfDeployPlanProtoCommon(plan.ProjectRef, plan.RepositoryRef, plan.ProviderSignalRef, plan.SourceRef, plan.MergeCommitSHA, plan.ServicesYaml, plan.AffectedServiceKeys, plan.PlanFingerprint, plan.SafeSummary, plan.Version)
	converted := &projectsv1.SelfDeployDeployPlan{}
	converted.ProjectRef = common.projectRef
	converted.RepositoryRef = common.repositoryRef
	converted.ProviderSignalRef = common.providerSignalRef
	converted.SourceRef = common.sourceRef
	converted.MergeCommitSha = common.mergeCommitSHA
	converted.ServicesYaml = common.servicesYAML
	converted.AffectedServiceKeys = common.affectedServiceKeys
	converted.DeployItems = mapSlice(plan.DeployItems, selfDeployDeployPlanItemToProto)
	converted.PlanFingerprint = common.planFingerprint
	converted.SafeSummary = common.safeSummary
	converted.Version = common.version
	return converted
}

type selfDeployPlanProtoCommonFields struct {
	projectRef          string
	repositoryRef       string
	providerSignalRef   *string
	sourceRef           string
	mergeCommitSHA      string
	servicesYAML        *projectsv1.SelfDeployServicesYamlProjection
	affectedServiceKeys []string
	planFingerprint     string
	safeSummary         string
	version             int64
}

func selfDeployPlanProtoCommon(projectRef string, repositoryRef string, providerSignalRef string, sourceRef string, mergeCommitSHA string, servicesYAML projectservice.SelfDeployServicesYamlProjection, affectedServiceKeys []string, planFingerprint string, safeSummary string, version int64) selfDeployPlanProtoCommonFields {
	return selfDeployPlanProtoCommonFields{
		projectRef:          projectRef,
		repositoryRef:       repositoryRef,
		providerSignalRef:   optionalStringPtr(providerSignalRef),
		sourceRef:           sourceRef,
		mergeCommitSHA:      mergeCommitSHA,
		servicesYAML:        selfDeployServicesYamlToProto(servicesYAML),
		affectedServiceKeys: affectedServiceKeys,
		planFingerprint:     planFingerprint,
		safeSummary:         safeSummary,
		version:             version,
	}
}

func selfDeployDeployPlanItemToProto(item projectservice.SelfDeployDeployPlanItem) *projectsv1.SelfDeployDeployPlanItem {
	return &projectsv1.SelfDeployDeployPlanItem{
		ServiceKey:          item.ServiceKey,
		ServiceRef:          optionalStringPtr(item.ServiceRef),
		DeployExecutionSpec: deployExecutionSpecToProto(item.DeployExecutionSpec),
		PlanItemFingerprint: item.PlanItemFingerprint,
		Status:              selfDeployDeployPlanItemStatusToProto(item.Status),
		SafeReason:          optionalStringPtr(item.SafeReason),
	}
}

func selfDeployDeployPlanItemStatusToProto(status projectservice.SelfDeployDeployPlanItemStatus) projectsv1.SelfDeployDeployPlanItemStatus {
	statuses := selfDeployDeployPlanItemStatusValues()
	if value, ok := statuses[status]; ok {
		return value
	}
	return projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_UNSPECIFIED
}

func selfDeployDeployPlanItemStatusValues() map[projectservice.SelfDeployDeployPlanItemStatus]projectsv1.SelfDeployDeployPlanItemStatus {
	values := make(map[projectservice.SelfDeployDeployPlanItemStatus]projectsv1.SelfDeployDeployPlanItemStatus, 4)
	values[projectservice.SelfDeployDeployPlanItemStatusReady] = projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_READY
	values[projectservice.SelfDeployDeployPlanItemStatusBuildNotReady] = projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_BUILD_NOT_READY
	values[projectservice.SelfDeployDeployPlanItemStatusBuildOutputInvalid] = projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_BUILD_OUTPUT_INVALID
	values[projectservice.SelfDeployDeployPlanItemStatusDeployPlanUnavailable] = projectsv1.SelfDeployDeployPlanItemStatus_SELF_DEPLOY_DEPLOY_PLAN_ITEM_STATUS_DEPLOY_PLAN_UNAVAILABLE
	return values
}

func deployExecutionSpecToProto(spec projectservice.SelfDeployDeployExecutionSpec) *runtimev1.DeployExecutionSpec {
	if spec.ServiceKey == "" && spec.ManifestBundleRef == "" {
		return nil
	}
	return &runtimev1.DeployExecutionSpec{
		SourceRef:             spec.SourceRef,
		SourceCommitSha:       spec.SourceCommitSHA,
		ServiceKey:            spec.ServiceKey,
		ImageRef:              spec.ImageRef,
		ImageTag:              spec.ImageTag,
		ImageDigest:           optionalStringPtr(spec.ImageDigest),
		ManifestRef:           spec.ManifestRef,
		ManifestDigest:        spec.ManifestDigest,
		KustomizationRef:      spec.KustomizationRef,
		KustomizationDigest:   spec.KustomizationDigest,
		TargetNamespace:       spec.TargetNamespace,
		TargetClusterRef:      spec.TargetClusterRef,
		TargetSlotId:          optionalStringPtr(spec.TargetSlotID),
		DeployPlanFingerprint: spec.DeployPlanFingerprint,
		AllowedSecretRefs:     mapSlice(spec.AllowedSecretRefs, allowedSecretRefToProto),
		OutputRefs:            mapSlice(spec.OutputRefs, outputRefToProto),
		ManifestBundleRef:     spec.ManifestBundleRef,
		ManifestBundleDigest:  spec.ManifestBundleDigest,
		RolloutTargets:        mapSlice(spec.RolloutTargets, rolloutTargetToProto),
		ExpectedImageRefs:     mapSlice(spec.ExpectedImageRefs, expectedImageRefToProto),
	}
}

func rolloutTargetToProto(target projectservice.SelfDeployDeployRolloutTarget) *runtimev1.DeployRolloutTarget {
	return &runtimev1.DeployRolloutTarget{
		Kind:      target.Kind,
		Ref:       target.Ref,
		Namespace: target.Namespace,
		Name:      target.Name,
		Digest:    optionalStringPtr(target.Digest),
	}
}

func expectedImageRefToProto(ref projectservice.SelfDeployDeployExpectedImageRef) *runtimev1.DeployExpectedImageRef {
	return &runtimev1.DeployExpectedImageRef{
		ContainerName: ref.ContainerName,
		ImageRef:      ref.ImageRef,
		ImageDigest:   optionalStringPtr(ref.ImageDigest),
	}
}

func BootstrapServicesPolicyImportResponse(result projectservice.BootstrapServicesPolicyImportResult) *projectsv1.BootstrapServicesPolicyImportResponse {
	return &projectsv1.BootstrapServicesPolicyImportResponse{
		Repository:      RepositoryToProto(result.Repository),
		ServicesPolicy:  ServicesPolicyToProto(result.ServicesPolicy),
		SourceRef:       result.SourceRef,
		SourceCommitSha: result.SourceCommitSHA,
		Summary:         result.Summary,
	}
}

func ServicesPolicyToProto(policy entity.ServicesPolicy) *projectsv1.ServicesPolicy {
	return &projectsv1.ServicesPolicy{
		ServicesPolicyId:     policy.ID.String(),
		ProjectId:            policy.ProjectID.String(),
		SourceRepositoryId:   uuidPtrString(policy.SourceRepositoryID),
		SourcePath:           policy.SourcePath,
		SourceRef:            stringPtr(policy.SourceRef),
		SourceCommitSha:      policy.SourceCommitSHA,
		SourceBlobSha:        stringPtr(policy.SourceBlobSHA),
		PolicyVersion:        policy.PolicyVersion,
		ContentHash:          policy.ContentHash,
		ValidatedPayloadJson: string(policy.ValidatedPayload),
		ValidationStatus:     ValidationStatusToProto(policy.ValidationStatus),
		ProjectionStatus:     ProjectionStatusToProto(policy.ProjectionStatus),
		ImportedAt:           formatTime(policy.ImportedAt),
	}
}

func ListServiceDescriptorsResponse(result projectservice.ListServiceDescriptorsResult) *projectsv1.ListServiceDescriptorsResponse {
	return &projectsv1.ListServiceDescriptorsResponse{ServiceDescriptors: mapSlice(result.ServiceDescriptors, ServiceDescriptorToProto), Page: pageResponseToProto(result.Page)}
}

func ServiceDescriptorToProto(descriptor entity.ServiceDescriptor) *projectsv1.ServiceDescriptor {
	return &projectsv1.ServiceDescriptor{
		ServiceDescriptorId:  descriptor.ID.String(),
		ProjectId:            descriptor.ProjectID.String(),
		ServicesPolicyId:     descriptor.ServicesPolicyID.String(),
		RepositoryId:         uuidPtrString(descriptor.RepositoryID),
		ServiceKey:           descriptor.ServiceKey,
		DisplayName:          descriptor.DisplayName,
		Kind:                 ServiceKindToProto(descriptor.Kind),
		RootPath:             descriptor.RootPath,
		DocumentationScopeId: stringPtr(descriptor.DocumentationScopeID),
		DependsOnServiceKeys: descriptor.DependsOnServiceKeys,
		Status:               ServiceStatusToProto(descriptor.Status),
		Version:              descriptor.Version,
	}
}

func PolicyEditProposalResponse(proposal entity.PolicyEditProposal) *projectsv1.PolicyEditProposalResponse {
	return &projectsv1.PolicyEditProposalResponse{
		ProposalId:   proposal.ID.String(),
		ProjectId:    proposal.ProjectID.String(),
		RepositoryId: proposal.RepositoryID.String(),
		Status:       proposal.Status,
	}
}

func PolicyOverrideResponse(override entity.PolicyOverride) *projectsv1.PolicyOverrideResponse {
	return &projectsv1.PolicyOverrideResponse{PolicyOverride: PolicyOverrideToProto(override)}
}

func PolicyOverrideToProto(override entity.PolicyOverride) *projectsv1.PolicyOverride {
	return &projectsv1.PolicyOverride{
		PolicyOverrideId:  override.ID.String(),
		ProjectId:         override.ProjectID.String(),
		TargetType:        PolicyOverrideTargetToProto(override.TargetType),
		TargetId:          uuidPtrString(override.TargetID),
		PayloadJson:       string(override.Payload),
		Reason:            override.Reason,
		Status:            PolicyOverrideStatusToProto(override.Status),
		ExpiresAt:         formatTime(override.ExpiresAt),
		CreatedByActorRef: override.CreatedByActorRef,
		Version:           override.Version,
	}
}

func ListPolicyOverridesResponse(result projectservice.ListPolicyOverridesResult) *projectsv1.ListPolicyOverridesResponse {
	return &projectsv1.ListPolicyOverridesResponse{PolicyOverrides: mapSlice(result.PolicyOverrides, PolicyOverrideToProto), Page: pageResponseToProto(result.Page)}
}

func DocumentationSourceResponse(source entity.DocumentationSource) *projectsv1.DocumentationSourceResponse {
	return &projectsv1.DocumentationSourceResponse{DocumentationSource: DocumentationSourceToProto(source)}
}

func DocumentationSourceToProto(source entity.DocumentationSource) *projectsv1.DocumentationSource {
	return &projectsv1.DocumentationSource{
		DocumentationSourceId: source.ID.String(),
		ProjectId:             source.ProjectID.String(),
		RepositoryId:          uuidPtrString(source.RepositoryID),
		ScopeType:             DocumentationScopeToProto(source.ScopeType),
		ScopeId:               stringPtr(source.ScopeID),
		LocalPath:             source.LocalPath,
		AccessMode:            DocumentationAccessToProto(source.AccessMode),
		Status:                DocumentationStatusToProto(source.Status),
		Version:               source.Version,
	}
}

func ListDocumentationSourcesResponse(result projectservice.ListDocumentationSourcesResult) *projectsv1.ListDocumentationSourcesResponse {
	return &projectsv1.ListDocumentationSourcesResponse{DocumentationSources: mapSlice(result.DocumentationSources, DocumentationSourceToProto), Page: pageResponseToProto(result.Page)}
}

func WorkspacePolicyResponse(policy entity.WorkspacePolicy) *projectsv1.WorkspacePolicyResponse {
	codeSources := make([]*projectsv1.WorkspaceCodeSource, 0, len(policy.CodeSources))
	for _, source := range policy.CodeSources {
		codeSources = append(codeSources, &projectsv1.WorkspaceCodeSource{
			RepositoryId:  source.RepositoryID.String(),
			Provider:      RepositoryProviderToProto(source.Provider),
			ProviderOwner: source.ProviderOwner,
			ProviderName:  source.ProviderName,
			DefaultBranch: source.DefaultBranch,
			LocalPath:     source.LocalPath,
			AccessMode:    sourceAccessToProto(source.AccessMode),
		})
	}
	documentationSources := make([]*projectsv1.WorkspaceDocumentationSource, 0, len(policy.DocumentationSources))
	for _, source := range policy.DocumentationSources {
		documentationSources = append(documentationSources, &projectsv1.WorkspaceDocumentationSource{
			DocumentationSourceId: source.DocumentationSourceID.String(),
			RepositoryId:          uuidPtrString(source.RepositoryID),
			ScopeType:             DocumentationScopeToProto(source.ScopeType),
			ScopeId:               stringPtr(source.ScopeID),
			LocalPath:             source.LocalPath,
			AccessMode:            sourceAccessToProto(source.AccessMode),
		})
	}
	overrides := make([]*projectsv1.PolicyOverride, 0, len(policy.ActivePolicyOverrides))
	for _, override := range policy.ActivePolicyOverrides {
		overrides = append(overrides, PolicyOverrideToProto(override))
	}
	return &projectsv1.WorkspacePolicyResponse{WorkspacePolicy: &projectsv1.WorkspacePolicy{
		ProjectId:             policy.ProjectID.String(),
		CodeSources:           codeSources,
		DocumentationSources:  documentationSources,
		GuidancePackageRefs:   policy.GuidancePackageRefs,
		PolicyVersion:         policy.PolicyVersion,
		ActivePolicyOverrides: overrides,
	}}
}

func BranchRulesResponse(rules entity.BranchRules) *projectsv1.BranchRulesResponse {
	return &projectsv1.BranchRulesResponse{BranchRules: BranchRulesToProto(rules)}
}

func BranchRulesToProto(rules entity.BranchRules) *projectsv1.BranchRules {
	return &projectsv1.BranchRules{
		BranchRulesId:  rules.ID.String(),
		ProjectId:      rules.ProjectID.String(),
		RepositoryId:   uuidPtrString(rules.RepositoryID),
		Pattern:        rules.Pattern,
		RequiredChecks: rules.RequiredChecks,
		MergePolicy:    MergePolicyToProto(rules.MergePolicy),
		Status:         BranchRulesStatusToProto(rules.Status),
		Version:        rules.Version,
	}
}

func ListBranchRulesResponse(result projectservice.ListBranchRulesResult) *projectsv1.ListBranchRulesResponse {
	return &projectsv1.ListBranchRulesResponse{BranchRules: mapSlice(result.BranchRules, BranchRulesToProto), Page: pageResponseToProto(result.Page)}
}

func ReleasePolicyResponse(policy entity.ReleasePolicy) *projectsv1.ReleasePolicyResponse {
	return &projectsv1.ReleasePolicyResponse{ReleasePolicy: ReleasePolicyToProto(policy)}
}

func ReleasePolicyToProto(policy entity.ReleasePolicy) *projectsv1.ReleasePolicy {
	return &projectsv1.ReleasePolicy{
		ReleasePolicyId: policy.ID.String(),
		ProjectId:       policy.ProjectID.String(),
		Name:            policy.Name,
		BranchPattern:   policy.BranchPattern,
		RolloutStrategy: RolloutStrategyToProto(policy.RolloutStrategy),
		RollbackPolicy:  RollbackPolicyToProto(policy.RollbackPolicy),
		RiskProfileRef:  stringPtr(policy.RiskProfileRef),
		Status:          ReleaseStatusToProto(policy.Status),
		Version:         policy.Version,
	}
}

func ListReleasePoliciesResponse(result projectservice.ListReleasePoliciesResult) *projectsv1.ListReleasePoliciesResponse {
	return &projectsv1.ListReleasePoliciesResponse{ReleasePolicies: mapSlice(result.ReleasePolicies, ReleasePolicyToProto), Page: pageResponseToProto(result.Page)}
}

func ReleaseLineResponse(line entity.ReleaseLine) *projectsv1.ReleaseLineResponse {
	return &projectsv1.ReleaseLineResponse{ReleaseLine: ReleaseLineToProto(line)}
}

func ReleaseLineToProto(line entity.ReleaseLine) *projectsv1.ReleaseLine {
	return &projectsv1.ReleaseLine{
		ReleaseLineId:   line.ID.String(),
		ProjectId:       line.ProjectID.String(),
		ReleasePolicyId: line.ReleasePolicyID.String(),
		Name:            line.Name,
		BranchPattern:   line.BranchPattern,
		Status:          ReleaseStatusToProto(line.Status),
		Version:         line.Version,
	}
}

func ListReleaseLinesResponse(result projectservice.ListReleaseLinesResult) *projectsv1.ListReleaseLinesResponse {
	return &projectsv1.ListReleaseLinesResponse{ReleaseLines: mapSlice(result.ReleaseLines, ReleaseLineToProto), Page: pageResponseToProto(result.Page)}
}

func PlacementPolicyResponse(policy entity.PlacementPolicy) *projectsv1.PlacementPolicyResponse {
	return &projectsv1.PlacementPolicyResponse{PlacementPolicy: PlacementPolicyToProto(policy)}
}

func PlacementPolicyToProto(policy entity.PlacementPolicy) *projectsv1.PlacementPolicy {
	return &projectsv1.PlacementPolicy{
		PlacementPolicyId:  policy.ID.String(),
		ProjectId:          policy.ProjectID.String(),
		RepositoryId:       uuidPtrString(policy.RepositoryID),
		ServiceKey:         stringPtr(policy.ServiceKey),
		AllowedClusterRefs: policy.AllowedClusterRefs,
		Status:             PlacementStatusToProto(policy.Status),
		Version:            policy.Version,
	}
}

func ListPlacementPoliciesResponse(result projectservice.ListPlacementPoliciesResult) *projectsv1.ListPlacementPoliciesResponse {
	return &projectsv1.ListPlacementPoliciesResponse{PlacementPolicies: mapSlice(result.PlacementPolicies, PlacementPolicyToProto), Page: pageResponseToProto(result.Page)}
}

func uuidPtrString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func mapSlice[Input any, Output any](items []Input, cast func(Input) *Output) []*Output {
	result := make([]*Output, 0, len(items))
	for _, item := range items {
		result = append(result, cast(item))
	}
	return result
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
