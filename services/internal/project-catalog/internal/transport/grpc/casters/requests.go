package casters

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

// CreateProjectInput maps a gRPC request to the domain command input.
func CreateProjectInput(request *projectsv1.CreateProjectRequest) (projectservice.CreateProjectInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.CreateProjectInput{}, err
	}
	organizationID, err := requiredUUID(request.GetOrganizationId())
	if err != nil {
		return projectservice.CreateProjectInput{}, err
	}
	status, err := projectStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.CreateProjectInput{}, err
	}
	return projectservice.CreateProjectInput{
		OrganizationID: organizationID,
		Slug:           strings.TrimSpace(request.GetSlug()),
		DisplayName:    strings.TrimSpace(request.GetDisplayName()),
		Description:    strings.TrimSpace(request.GetDescription()),
		IconObjectURI:  strings.TrimSpace(request.GetIconObjectUri()),
		Status:         status,
		Meta:           meta,
	}, nil
}

// UpdateProjectInput maps a gRPC request to the domain command input.
func UpdateProjectInput(request *projectsv1.UpdateProjectRequest) (projectservice.UpdateProjectInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.UpdateProjectInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.UpdateProjectInput{}, err
	}
	status, err := projectStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.UpdateProjectInput{}, err
	}
	return projectservice.UpdateProjectInput{
		ProjectID:     projectID,
		Slug:          request.Slug,
		DisplayName:   request.DisplayName,
		Description:   request.Description,
		IconObjectURI: request.IconObjectUri,
		Status:        status,
		Meta:          meta,
	}, nil
}

// GetProjectInput maps a gRPC request to id and query metadata.
func GetProjectInput(request *projectsv1.GetProjectRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetProjectId(), request.GetMeta())
}

// ListProjectsInput maps a gRPC request to the domain read input.
func ListProjectsInput(request *projectsv1.ListProjectsRequest) (projectservice.ListProjectsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.ListProjectsInput{}, err
	}
	organizationID, err := optionalUUIDPtr(request.GetOrganizationId())
	if err != nil {
		return projectservice.ListProjectsInput{}, err
	}
	statuses, err := projectStatusesFromProto(request.GetStatuses())
	if err != nil {
		return projectservice.ListProjectsInput{}, err
	}
	return projectservice.ListProjectsInput{OrganizationID: organizationID, Statuses: statuses, Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

// AttachRepositoryInput maps a gRPC request to the domain command input.
func AttachRepositoryInput(request *projectsv1.AttachRepositoryRequest) (projectservice.AttachRepositoryInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.AttachRepositoryInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.AttachRepositoryInput{}, err
	}
	provider, err := repositoryProviderFromProto(request.GetProvider())
	if err != nil {
		return projectservice.AttachRepositoryInput{}, err
	}
	status, err := repositoryStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.AttachRepositoryInput{}, err
	}
	return projectservice.AttachRepositoryInput{
		ProjectID:            projectID,
		Provider:             provider,
		ProviderOwner:        strings.TrimSpace(request.GetProviderOwner()),
		ProviderName:         strings.TrimSpace(request.GetProviderName()),
		WebURL:               strings.TrimSpace(request.GetWebUrl()),
		DefaultBranch:        strings.TrimSpace(request.GetDefaultBranch()),
		ProviderRepositoryID: strings.TrimSpace(request.GetProviderRepositoryId()),
		IconObjectURI:        strings.TrimSpace(request.GetIconObjectUri()),
		Status:               status,
		Meta:                 meta,
	}, nil
}

// CreateProviderRepositoryInput maps a provider repository create request to the domain command input.
func CreateProviderRepositoryInput(request *projectsv1.CreateProviderRepositoryRequest) (projectservice.CreateProviderRepositoryInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.CreateProviderRepositoryInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.CreateProviderRepositoryInput{}, err
	}
	provider, err := repositoryProviderFromProto(request.GetProvider())
	if err != nil {
		return projectservice.CreateProviderRepositoryInput{}, err
	}
	ownerKind, err := repositoryOwnerKindFromProto(request.GetOwnerKind())
	if err != nil {
		return projectservice.CreateProviderRepositoryInput{}, err
	}
	visibility, err := repositoryVisibilityFromProto(request.GetVisibility())
	if err != nil {
		return projectservice.CreateProviderRepositoryInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return projectservice.CreateProviderRepositoryInput{}, err
	}
	return projectservice.CreateProviderRepositoryInput{
		ProjectID:         projectID,
		Provider:          provider,
		OwnerKind:         ownerKind,
		ProviderOwner:     strings.TrimSpace(request.GetProviderOwner()),
		ProviderName:      strings.TrimSpace(request.GetProviderName()),
		Visibility:        visibility,
		Description:       strings.TrimSpace(request.GetDescription()),
		IconObjectURI:     strings.TrimSpace(request.GetIconObjectUri()),
		ExternalAccountID: externalAccountID,
		Meta:              meta,
	}, nil
}

// CreateRepositoryBootstrapPullRequestInput maps a bootstrap PR request to the domain command input.
func CreateRepositoryBootstrapPullRequestInput(request *projectsv1.CreateRepositoryBootstrapPullRequestRequest) (projectservice.CreateRepositoryBootstrapPullRequestInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.CreateRepositoryBootstrapPullRequestInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.CreateRepositoryBootstrapPullRequestInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return projectservice.CreateRepositoryBootstrapPullRequestInput{}, err
	}
	externalAccountID, err := requiredUUID(request.GetExternalAccountId())
	if err != nil {
		return projectservice.CreateRepositoryBootstrapPullRequestInput{}, err
	}
	servicesPolicy := request.GetServicesPolicy()
	if servicesPolicy == nil {
		return projectservice.CreateRepositoryBootstrapPullRequestInput{}, errs.ErrInvalidArgument
	}
	return projectservice.CreateRepositoryBootstrapPullRequestInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		BaseBranch:        strings.TrimSpace(request.GetBaseBranch()),
		BootstrapBranch:   strings.TrimSpace(request.GetBootstrapBranch()),
		CommitMessage:     strings.TrimSpace(request.GetCommitMessage()),
		Title:             strings.TrimSpace(request.GetTitle()),
		Body:              strings.TrimSpace(request.GetBody()),
		Draft:             request.GetDraft(),
		Files:             bootstrapFilesFromProto(request.GetFiles()),
		WatermarkJSON:     []byte(strings.TrimSpace(request.GetWatermarkJson())),
		ServicesPolicy:    bootstrapServicesPolicyFromProto(servicesPolicy),
		ExternalAccountID: externalAccountID,
		Meta:              meta,
	}, nil
}

func bootstrapFilesFromProto(files []*projectsv1.RepositoryBootstrapFile) []projectservice.RepositoryBootstrapFile {
	result := make([]projectservice.RepositoryBootstrapFile, 0, len(files))
	for _, file := range files {
		if file == nil {
			continue
		}
		result = append(result, projectservice.RepositoryBootstrapFile{
			Path:       strings.TrimSpace(file.GetPath()),
			Content:    file.GetContent(),
			Executable: file.GetExecutable(),
		})
	}
	return result
}

func bootstrapServicesPolicyFromProto(policy *projectsv1.RepositoryBootstrapServicesPolicy) projectservice.RepositoryBootstrapServicesPolicy {
	return projectservice.RepositoryBootstrapServicesPolicy{
		SourcePath:       strings.TrimSpace(policy.GetSourcePath()),
		ContentHash:      strings.TrimSpace(policy.GetContentHash()),
		ValidatedPayload: []byte(strings.TrimSpace(policy.GetValidatedPayloadJson())),
	}
}

// UpdateRepositoryInput maps a gRPC request to the domain command input.
func UpdateRepositoryInput(request *projectsv1.UpdateRepositoryRequest) (projectservice.UpdateRepositoryInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.UpdateRepositoryInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return projectservice.UpdateRepositoryInput{}, err
	}
	status, err := repositoryStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.UpdateRepositoryInput{}, err
	}
	return projectservice.UpdateRepositoryInput{
		RepositoryID:  repositoryID,
		DefaultBranch: request.DefaultBranch,
		Status:        status,
		IconObjectURI: request.IconObjectUri,
		Meta:          meta,
	}, nil
}

// DetachRepositoryInput maps a gRPC request to id and command metadata.
func DetachRepositoryInput(request *projectsv1.DetachRepositoryRequest) (uuid.UUID, value.CommandMeta, error) {
	return idWithCommandMeta(request.GetRepositoryId(), request.GetMeta())
}

func GetRepositoryInput(request *projectsv1.GetRepositoryRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetRepositoryId(), request.GetMeta())
}

func ListRepositoriesInput(request *projectsv1.ListRepositoriesRequest) (projectservice.ListRepositoriesInput, error) {
	return listProjectStatusesInput(
		request.GetProjectId(),
		request.GetMeta(),
		request.GetPage(),
		request.GetStatuses(),
		repositoryStatusesFromProto,
		buildListRepositoriesInput,
	)
}

func ImportServicesPolicyInput(request *projectsv1.ImportServicesPolicyRequest) (projectservice.ImportServicesPolicyInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.ImportServicesPolicyInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.ImportServicesPolicyInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetSourceRepositoryId())
	if err != nil {
		return projectservice.ImportServicesPolicyInput{}, err
	}
	status, err := validationStatusFromProto(request.GetValidationStatus())
	if err != nil {
		return projectservice.ImportServicesPolicyInput{}, err
	}
	descriptors, err := serviceDescriptorsFromProto(request.GetServiceDescriptors())
	if err != nil {
		return projectservice.ImportServicesPolicyInput{}, err
	}
	return projectservice.ImportServicesPolicyInput{
		ProjectID:          projectID,
		SourceRepositoryID: repositoryID,
		SourcePath:         strings.TrimSpace(request.GetSourcePath()),
		SourceRef:          strings.TrimSpace(request.GetSourceRef()),
		SourceCommitSHA:    strings.TrimSpace(request.GetSourceCommitSha()),
		SourceBlobSHA:      strings.TrimSpace(request.GetSourceBlobSha()),
		ContentHash:        strings.TrimSpace(request.GetContentHash()),
		ValidatedPayload:   []byte(strings.TrimSpace(request.GetValidatedPayloadJson())),
		ServiceDescriptors: descriptors,
		ValidationStatus:   status,
		Meta:               meta,
	}, nil
}

func ImportBootstrapServicesPolicyInput(request *projectsv1.ImportBootstrapServicesPolicyRequest) (projectservice.ImportBootstrapServicesPolicyInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.ImportBootstrapServicesPolicyInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.ImportBootstrapServicesPolicyInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return projectservice.ImportBootstrapServicesPolicyInput{}, err
	}
	providerTarget := request.GetProviderTarget()
	if providerTarget == nil {
		return projectservice.ImportBootstrapServicesPolicyInput{}, errs.ErrInvalidArgument
	}
	return projectservice.ImportBootstrapServicesPolicyInput{
		ProjectID:                    projectID,
		RepositoryID:                 repositoryID,
		ProviderTarget:               bootstrapProviderTargetFromProto(providerTarget),
		BaseBranch:                   strings.TrimSpace(request.GetBaseBranch()),
		SourceRef:                    strings.TrimSpace(request.GetSourceRef()),
		SourceCommitSHA:              strings.TrimSpace(request.GetSourceCommitSha()),
		SourceBlobSHA:                strings.TrimSpace(request.GetSourceBlobSha()),
		SourcePath:                   strings.TrimSpace(request.GetSourcePath()),
		ContentHash:                  strings.TrimSpace(request.GetContentHash()),
		ValidatedPayload:             []byte(strings.TrimSpace(request.GetValidatedPayloadJson())),
		WatermarkJSON:                []byte(strings.TrimSpace(request.GetWatermarkJson())),
		ProviderWorkItemProjectionID: strings.TrimSpace(request.GetProviderWorkItemProjectionId()),
		ProviderWebURL:               strings.TrimSpace(request.GetProviderWebUrl()),
		ProviderObjectID:             strings.TrimSpace(request.GetProviderObjectId()),
		MergeObservedAt:              strings.TrimSpace(request.GetMergeObservedAt()),
		Meta:                         meta,
	}, nil
}

func ReconcileBootstrapMergeSignalInput(request *projectsv1.ReconcileBootstrapMergeSignalRequest) (projectservice.ReconcileBootstrapMergeSignalInput, error) {
	projectID, repositoryID, mergeSignal, checkedPolicy, meta, err := repositoryMergeSignalInput(
		request.GetProjectId(),
		request.GetRepositoryId(),
		request.GetMergeSignal(),
		request.GetCheckedPolicy(),
		request.GetMeta(),
	)
	if err != nil {
		return projectservice.ReconcileBootstrapMergeSignalInput{}, err
	}
	return projectservice.ReconcileBootstrapMergeSignalInput{
		ProjectID:     projectID,
		RepositoryID:  repositoryID,
		MergeSignal:   mergeSignal,
		CheckedPolicy: checkedPolicy,
		Meta:          meta,
	}, nil
}

func ReconcileAdoptionMergeSignalInput(request *projectsv1.ReconcileAdoptionMergeSignalRequest) (projectservice.ReconcileAdoptionMergeSignalInput, error) {
	projectID, repositoryID, mergeSignal, checkedPolicy, meta, err := repositoryAdoptionMergeSignalInput(request)
	if err != nil {
		return projectservice.ReconcileAdoptionMergeSignalInput{}, err
	}
	return projectservice.ReconcileAdoptionMergeSignalInput{
		ProjectID:     projectID,
		RepositoryID:  repositoryID,
		MergeSignal:   projectservice.RepositoryAdoptionMergeSignal(mergeSignal),
		CheckedPolicy: checkedPolicy,
		Meta:          meta,
	}, nil
}

func bootstrapProviderTargetFromProto(target *projectsv1.RepositoryBootstrapProviderTarget) projectservice.RepositoryBootstrapProviderTarget {
	return projectservice.RepositoryBootstrapProviderTarget{
		ProviderSlug:         strings.TrimSpace(target.GetProviderSlug()),
		RepositoryFullName:   strings.TrimSpace(target.GetRepositoryFullName()),
		ProviderRepositoryID: strings.TrimSpace(target.GetProviderRepositoryId()),
		WebURL:               strings.TrimSpace(target.GetWebUrl()),
	}
}

func repositoryMergeSignalInput(
	projectIDValue string,
	repositoryIDValue string,
	signal *projectsv1.BootstrapRepositoryMergeSignal,
	checkedPolicy *projectsv1.CheckedBootstrapServicesPolicyArtifact,
	metaValue *projectsv1.CommandMeta,
) (uuid.UUID, uuid.UUID, projectservice.BootstrapRepositoryMergeSignal, projectservice.CheckedBootstrapServicesPolicyArtifact, value.CommandMeta, error) {
	meta, err := CommandMetaFromProto(metaValue)
	if err != nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, err
	}
	projectID, err := requiredUUID(projectIDValue)
	if err != nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, err
	}
	repositoryID, err := requiredUUID(repositoryIDValue)
	if err != nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, err
	}
	if signal == nil || checkedPolicy == nil || signal.GetProviderTarget() == nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, errs.ErrInvalidArgument
	}
	return projectID, repositoryID, repositoryMergeSignalFromProto(signal), checkedRepositoryServicesPolicyFromProto(
		checkedPolicy.GetArtifactRef(),
		checkedPolicy.GetArtifactDigest(),
		checkedPolicy.GetArtifactVersion(),
		checkedPolicy.GetSourcePath(),
		checkedPolicy.GetContentHash(),
		checkedPolicy.GetValidatedPayloadJson(),
	), meta, nil
}

func repositoryAdoptionMergeSignalInput(request *projectsv1.ReconcileAdoptionMergeSignalRequest) (uuid.UUID, uuid.UUID, projectservice.BootstrapRepositoryMergeSignal, projectservice.CheckedBootstrapServicesPolicyArtifact, value.CommandMeta, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, err
	}
	signal := request.GetMergeSignal()
	checkedPolicy := request.GetCheckedPolicy()
	if signal == nil || checkedPolicy == nil || signal.GetProviderTarget() == nil {
		return uuid.Nil, uuid.Nil, projectservice.BootstrapRepositoryMergeSignal{}, projectservice.CheckedBootstrapServicesPolicyArtifact{}, value.CommandMeta{}, errs.ErrInvalidArgument
	}
	return projectID, repositoryID, repositoryMergeSignalFromProto(signal), checkedRepositoryServicesPolicyFromProto(
		checkedPolicy.GetArtifactRef(),
		checkedPolicy.GetArtifactDigest(),
		checkedPolicy.GetArtifactVersion(),
		checkedPolicy.GetSourcePath(),
		checkedPolicy.GetContentHash(),
		checkedPolicy.GetValidatedPayloadJson(),
	), meta, nil
}

type repositoryMergeSignalProto interface {
	GetSignalId() string
	GetSignalKey() string
	GetSignalKind() string
	GetProviderTarget() *projectsv1.RepositoryBootstrapProviderTarget
	GetBaseBranch() string
	GetSourceRef() string
	GetMergeCommitSha() string
	GetSourceBlobSha() string
	GetWatermarkDigest() string
	GetWatermarkJson() string
	GetProviderWorkItemProjectionId() string
	GetProviderWebUrl() string
	GetProviderObjectId() string
	GetMergeObservedAt() string
	GetMergedAt() string
}

func repositoryMergeSignalFromProto(signal repositoryMergeSignalProto) projectservice.BootstrapRepositoryMergeSignal {
	return projectservice.BootstrapRepositoryMergeSignal{
		SignalID:                     strings.TrimSpace(signal.GetSignalId()),
		SignalKey:                    strings.TrimSpace(signal.GetSignalKey()),
		SignalKind:                   strings.TrimSpace(signal.GetSignalKind()),
		ProviderTarget:               bootstrapProviderTargetFromProto(signal.GetProviderTarget()),
		BaseBranch:                   strings.TrimSpace(signal.GetBaseBranch()),
		SourceRef:                    strings.TrimSpace(signal.GetSourceRef()),
		MergeCommitSHA:               strings.TrimSpace(signal.GetMergeCommitSha()),
		SourceBlobSHA:                strings.TrimSpace(signal.GetSourceBlobSha()),
		WatermarkDigest:              strings.TrimSpace(signal.GetWatermarkDigest()),
		WatermarkJSON:                []byte(strings.TrimSpace(signal.GetWatermarkJson())),
		ProviderWorkItemProjectionID: strings.TrimSpace(signal.GetProviderWorkItemProjectionId()),
		ProviderWebURL:               strings.TrimSpace(signal.GetProviderWebUrl()),
		ProviderObjectID:             strings.TrimSpace(signal.GetProviderObjectId()),
		MergeObservedAt:              strings.TrimSpace(signal.GetMergeObservedAt()),
		MergedAt:                     strings.TrimSpace(signal.GetMergedAt()),
	}
}

func checkedRepositoryServicesPolicyFromProto(artifactRef string, artifactDigest string, artifactVersion string, sourcePath string, contentHash string, payload string) projectservice.CheckedBootstrapServicesPolicyArtifact {
	return projectservice.CheckedBootstrapServicesPolicyArtifact{
		ArtifactRef:      strings.TrimSpace(artifactRef),
		ArtifactDigest:   strings.TrimSpace(artifactDigest),
		ArtifactVersion:  strings.TrimSpace(artifactVersion),
		SourcePath:       strings.TrimSpace(sourcePath),
		ContentHash:      strings.TrimSpace(contentHash),
		ValidatedPayload: []byte(strings.TrimSpace(payload)),
	}
}

func GetServicesPolicyInput(request *projectsv1.GetServicesPolicyRequest) (projectservice.GetServicesPolicyInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.GetServicesPolicyInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.GetServicesPolicyInput{}, err
	}
	policyID, err := optionalUUIDPtr(request.GetServicesPolicyId())
	if err != nil {
		return projectservice.GetServicesPolicyInput{}, err
	}
	return projectservice.GetServicesPolicyInput{ProjectID: projectID, ServicesPolicyID: policyID, Meta: meta}, nil
}

func GetSelfDeploySignalInput(request *projectsv1.GetSelfDeploySignalRequest) (projectservice.GetSelfDeploySignalInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.GetSelfDeploySignalInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.GetSelfDeploySignalInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.GetSelfDeploySignalInput{}, err
	}
	return projectservice.GetSelfDeploySignalInput{
		ProjectID:         projectID,
		RepositoryID:      repositoryID,
		ProviderSignalID:  strings.TrimSpace(request.GetProviderSignalId()),
		ProviderSignalKey: strings.TrimSpace(request.GetProviderSignalKey()),
		Meta:              meta,
	}, nil
}

func GetSelfDeployBuildPlanInput(request *projectsv1.GetSelfDeployBuildPlanRequest) (projectservice.GetSelfDeployBuildPlanInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.GetSelfDeployBuildPlanInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.GetSelfDeployBuildPlanInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return projectservice.GetSelfDeployBuildPlanInput{}, err
	}
	var expectedVersion *int64
	if request.ExpectedServicesPolicyVersion != nil {
		value := request.GetExpectedServicesPolicyVersion()
		expectedVersion = &value
	}
	return projectservice.GetSelfDeployBuildPlanInput{
		ProjectID:                         projectID,
		RepositoryID:                      repositoryID,
		SourceRef:                         strings.TrimSpace(request.GetSourceRef()),
		MergeCommitSHA:                    strings.TrimSpace(request.GetMergeCommitSha()),
		ProviderSignalRef:                 strings.TrimSpace(request.GetProviderSignalRef()),
		ProviderSignalID:                  strings.TrimSpace(request.GetProviderSignalId()),
		ProviderSignalKey:                 strings.TrimSpace(request.GetProviderSignalKey()),
		AffectedServiceKeys:               request.GetAffectedServiceKeys(),
		ExpectedServicesPolicyDigest:      strings.TrimSpace(request.GetExpectedServicesPolicyDigest()),
		ExpectedServicesPolicyFingerprint: strings.TrimSpace(request.GetExpectedServicesPolicyFingerprint()),
		ExpectedServicesPolicyVersion:     expectedVersion,
		ExpectedBuildPlanFingerprint:      strings.TrimSpace(request.GetExpectedBuildPlanFingerprint()),
		MaterializedBuildContexts:         materializedBuildContextsFromProto(request.GetMaterializedBuildContexts()),
		Meta:                              meta,
	}, nil
}

func materializedBuildContextsFromProto(values []*projectsv1.SelfDeployMaterializedBuildContext) []projectservice.SelfDeployMaterializedBuildContext {
	result := make([]projectservice.SelfDeployMaterializedBuildContext, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		context := projectservice.SelfDeployMaterializedBuildContext{}
		context.ServiceKey = strings.TrimSpace(value.GetServiceKey())
		context.PlanItemFingerprint = strings.TrimSpace(value.GetPlanItemFingerprint())
		context.BuildContextRef = strings.TrimSpace(value.GetBuildContextRef())
		context.BuildContextDigest = strings.TrimSpace(value.GetBuildContextDigest())
		context.DockerfileDigest = strings.TrimSpace(value.GetDockerfileDigest())
		context.MaterializationRef = strings.TrimSpace(value.GetMaterializationRef())
		context.MaterializationFingerprint = strings.TrimSpace(value.GetMaterializationFingerprint())
		context.ManifestBundleDigest = strings.TrimSpace(value.GetManifestBundleDigest())
		result = append(result, context)
	}
	return result
}

func GetSelfDeployDeployPlanInput(request *projectsv1.GetSelfDeployDeployPlanRequest) (projectservice.GetSelfDeployDeployPlanInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.GetSelfDeployDeployPlanInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.GetSelfDeployDeployPlanInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return projectservice.GetSelfDeployDeployPlanInput{}, err
	}
	var expectedVersion *int64
	if request.ExpectedServicesPolicyVersion != nil {
		value := request.GetExpectedServicesPolicyVersion()
		expectedVersion = &value
	}
	return projectservice.GetSelfDeployDeployPlanInput{
		ProjectID:                         projectID,
		RepositoryID:                      repositoryID,
		SourceRef:                         strings.TrimSpace(request.GetSourceRef()),
		MergeCommitSHA:                    strings.TrimSpace(request.GetMergeCommitSha()),
		ProviderSignalRef:                 strings.TrimSpace(request.GetProviderSignalRef()),
		AffectedServiceKeys:               request.GetAffectedServiceKeys(),
		ExpectedServicesPolicyDigest:      strings.TrimSpace(request.GetExpectedServicesPolicyDigest()),
		ExpectedServicesPolicyFingerprint: strings.TrimSpace(request.GetExpectedServicesPolicyFingerprint()),
		ExpectedServicesPolicyVersion:     expectedVersion,
		ExpectedBuildPlanFingerprint:      strings.TrimSpace(request.GetExpectedBuildPlanFingerprint()),
		ExpectedDeployPlanFingerprint:     strings.TrimSpace(request.GetExpectedDeployPlanFingerprint()),
		BuildOutputs:                      buildOutputsFromProto(request.GetBuildOutputs()),
		MaterializedBuildContexts:         materializedBuildContextsFromProto(request.GetMaterializedBuildContexts()),
		Meta:                              meta,
	}, nil
}

func buildOutputsFromProto(values []*projectsv1.SelfDeployBuildOutput) []projectservice.SelfDeployBuildOutput {
	result := make([]projectservice.SelfDeployBuildOutput, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		result = append(result, projectservice.SelfDeployBuildOutput{
			ServiceKey:               strings.TrimSpace(value.GetServiceKey()),
			RuntimeJobRef:            strings.TrimSpace(value.GetRuntimeJobRef()),
			ImageRef:                 strings.TrimSpace(value.GetImageRef()),
			ImageTag:                 strings.TrimSpace(value.GetImageTag()),
			ImageDigest:              strings.TrimSpace(value.GetImageDigest()),
			BuildPlanItemFingerprint: strings.TrimSpace(value.GetBuildPlanItemFingerprint()),
			BuildPlanFingerprint:     strings.TrimSpace(value.GetBuildPlanFingerprint()),
			BuildContextRef:          strings.TrimSpace(value.GetBuildContextRef()),
			BuildContextDigest:       strings.TrimSpace(value.GetBuildContextDigest()),
		})
	}
	return result
}

func ListServiceDescriptorsInput(request *projectsv1.ListServiceDescriptorsRequest) (projectservice.ListServiceDescriptorsInput, error) {
	projectID, meta, err := idWithQueryMeta(request.GetProjectId(), request.GetMeta())
	if err != nil {
		return projectservice.ListServiceDescriptorsInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.ListServiceDescriptorsInput{}, err
	}
	statuses, err := serviceStatusesFromProto(request.GetStatuses())
	if err != nil {
		return projectservice.ListServiceDescriptorsInput{}, err
	}
	return projectservice.ListServiceDescriptorsInput{ProjectID: projectID, RepositoryID: repositoryID, ServiceKeys: trimStrings(request.GetServiceKeys()), Statuses: statuses, Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

func GetProjectOnboardingStatusInput(request *projectsv1.GetProjectOnboardingStatusRequest) (projectservice.GetProjectOnboardingStatusInput, error) {
	projectID, meta, err := idWithQueryMeta(request.GetProjectId(), request.GetMeta())
	if err != nil {
		return projectservice.GetProjectOnboardingStatusInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.GetProjectOnboardingStatusInput{}, err
	}
	policyID, err := optionalUUIDPtr(request.GetExpectedServicesPolicyId())
	if err != nil {
		return projectservice.GetProjectOnboardingStatusInput{}, err
	}
	var expectedVersion *int64
	if request.ExpectedServicesPolicyVersion != nil {
		value := request.GetExpectedServicesPolicyVersion()
		expectedVersion = &value
	}
	return projectservice.GetProjectOnboardingStatusInput{
		ProjectID:                     projectID,
		RepositoryID:                  repositoryID,
		ServiceKeys:                   trimStrings(request.GetServiceKeys()),
		ExpectedSourceRef:             strings.TrimSpace(request.GetExpectedSourceRef()),
		ExpectedSourceCommitSHA:       strings.TrimSpace(request.GetExpectedSourceCommitSha()),
		ExpectedContentHash:           strings.TrimSpace(request.GetExpectedContentHash()),
		ExpectedServicesPolicyID:      policyID,
		ExpectedServicesPolicyVersion: expectedVersion,
		Meta:                          meta,
	}, nil
}

func CreatePolicyEditProposalInput(request *projectsv1.CreatePolicyEditProposalRequest) (projectservice.CreatePolicyEditProposalInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.CreatePolicyEditProposalInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.CreatePolicyEditProposalInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return projectservice.CreatePolicyEditProposalInput{}, err
	}
	var changes value.PolicyEditProposalRequestedChanges
	if err := json.Unmarshal([]byte(strings.TrimSpace(request.GetRequestedChangesJson())), &changes); err != nil {
		return projectservice.CreatePolicyEditProposalInput{}, errs.ErrInvalidArgument
	}
	return projectservice.CreatePolicyEditProposalInput{ProjectID: projectID, RepositoryID: repositoryID, SourcePath: strings.TrimSpace(request.GetSourcePath()), RequestedChanges: changes, Meta: meta}, nil
}

func CreatePolicyOverrideInput(request *projectsv1.CreatePolicyOverrideRequest) (projectservice.CreatePolicyOverrideInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.CreatePolicyOverrideInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.CreatePolicyOverrideInput{}, err
	}
	targetType, err := policyOverrideTargetFromProto(request.GetTargetType())
	if err != nil {
		return projectservice.CreatePolicyOverrideInput{}, err
	}
	targetID, err := optionalUUIDPtr(request.GetTargetId())
	if err != nil {
		return projectservice.CreatePolicyOverrideInput{}, err
	}
	return projectservice.CreatePolicyOverrideInput{ProjectID: projectID, TargetType: targetType, TargetID: targetID, Payload: []byte(strings.TrimSpace(request.GetPayloadJson())), ExpiresAt: request.GetExpiresAt(), Meta: meta}, nil
}

func CancelPolicyOverrideInput(request *projectsv1.CancelPolicyOverrideRequest) (projectservice.CancelPolicyOverrideInput, error) {
	policyOverrideID, meta, err := idWithCommandMeta(request.GetPolicyOverrideId(), request.GetMeta())
	if err != nil {
		return projectservice.CancelPolicyOverrideInput{}, err
	}
	return projectservice.CancelPolicyOverrideInput{PolicyOverrideID: policyOverrideID, Meta: meta}, nil
}

func ListPolicyOverridesInput(request *projectsv1.ListPolicyOverridesRequest) (projectservice.ListPolicyOverridesInput, error) {
	projectID, meta, err := idWithQueryMeta(request.GetProjectId(), request.GetMeta())
	if err != nil {
		return projectservice.ListPolicyOverridesInput{}, err
	}
	targetID, err := optionalUUIDPtr(request.GetTargetId())
	if err != nil {
		return projectservice.ListPolicyOverridesInput{}, err
	}
	targetTypes, err := policyOverrideTargetsFromProto(request.GetTargetTypes())
	if err != nil {
		return projectservice.ListPolicyOverridesInput{}, err
	}
	statuses, err := policyOverrideStatusesFromProto(request.GetStatuses())
	if err != nil {
		return projectservice.ListPolicyOverridesInput{}, err
	}
	return projectservice.ListPolicyOverridesInput{ProjectID: projectID, TargetTypes: targetTypes, TargetID: targetID, Statuses: statuses, ActiveOnly: request.GetActiveOnly(), Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

func PutDocumentationSourceInput(request *projectsv1.PutDocumentationSourceRequest) (projectservice.PutDocumentationSourceInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	sourceID, err := optionalUUIDPtr(request.GetDocumentationSourceId())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	scopeType, err := documentationScopeFromProto(request.GetScopeType())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	accessMode, err := documentationAccessFromProto(request.GetAccessMode())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	status, err := documentationStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.PutDocumentationSourceInput{}, err
	}
	return projectservice.PutDocumentationSourceInput{
		DocumentationSourceID: sourceID,
		ProjectID:             projectID,
		RepositoryID:          repositoryID,
		ScopeType:             scopeType,
		ScopeID:               strings.TrimSpace(request.GetScopeId()),
		LocalPath:             strings.TrimSpace(request.GetLocalPath()),
		AccessMode:            accessMode,
		Status:                status,
		Meta:                  meta,
	}, nil
}

func GetDocumentationSourceInput(request *projectsv1.GetDocumentationSourceRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetDocumentationSourceId(), request.GetMeta())
}

func ListDocumentationSourcesInput(request *projectsv1.ListDocumentationSourcesRequest) (projectservice.ListDocumentationSourcesInput, error) {
	projectID, meta, err := idWithQueryMeta(request.GetProjectId(), request.GetMeta())
	if err != nil {
		return projectservice.ListDocumentationSourcesInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.ListDocumentationSourcesInput{}, err
	}
	scopeType, err := documentationScopeFromProto(request.GetScopeType())
	if err != nil {
		return projectservice.ListDocumentationSourcesInput{}, err
	}
	statuses, err := documentationStatusesFromProto(request.GetStatuses())
	if err != nil {
		return projectservice.ListDocumentationSourcesInput{}, err
	}
	return projectservice.ListDocumentationSourcesInput{ProjectID: projectID, RepositoryID: repositoryID, ScopeType: scopeType, ScopeID: strings.TrimSpace(request.GetScopeId()), Statuses: statuses, Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

func GetWorkspacePolicyInput(request *projectsv1.GetWorkspacePolicyRequest) (projectservice.GetWorkspacePolicyInput, error) {
	projectID, meta, err := idWithQueryMeta(request.GetProjectId(), request.GetMeta())
	if err != nil {
		return projectservice.GetWorkspacePolicyInput{}, err
	}
	repositoryIDs, err := requiredUUIDs(request.GetRepositoryIds())
	if err != nil {
		return projectservice.GetWorkspacePolicyInput{}, err
	}
	return projectservice.GetWorkspacePolicyInput{ProjectID: projectID, RepositoryIDs: repositoryIDs, ServiceKeys: trimStrings(request.GetServiceKeys()), IncludeGuidancePackages: request.GetIncludeGuidancePackages(), Meta: meta}, nil
}

func PutBranchRulesInput(request *projectsv1.PutBranchRulesRequest) (projectservice.PutBranchRulesInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.PutBranchRulesInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetBranchRulesId())
	if err != nil {
		return projectservice.PutBranchRulesInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.PutBranchRulesInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.PutBranchRulesInput{}, err
	}
	mergePolicy, err := mergePolicyFromProto(request.GetMergePolicy())
	if err != nil {
		return projectservice.PutBranchRulesInput{}, err
	}
	status, err := branchRulesStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.PutBranchRulesInput{}, err
	}
	return projectservice.PutBranchRulesInput{BranchRulesID: id, ProjectID: projectID, RepositoryID: repositoryID, Pattern: strings.TrimSpace(request.GetPattern()), RequiredChecks: trimStrings(request.GetRequiredChecks()), MergePolicy: mergePolicy, Status: status, Meta: meta}, nil
}

func GetBranchRulesInput(request *projectsv1.GetBranchRulesRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetBranchRulesId(), request.GetMeta())
}

func ListBranchRulesInput(request *projectsv1.ListBranchRulesRequest) (projectservice.ListBranchRulesInput, error) {
	return listProjectOptionalIDStatusesInput(
		request.GetProjectId(),
		request.GetRepositoryId(),
		request.GetMeta(),
		request.GetPage(),
		request.GetStatuses(),
		branchRulesStatusesFromProto,
		buildListBranchRulesInput,
	)
}

func PutReleasePolicyInput(request *projectsv1.PutReleasePolicyRequest) (projectservice.PutReleasePolicyInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.PutReleasePolicyInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetReleasePolicyId())
	if err != nil {
		return projectservice.PutReleasePolicyInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.PutReleasePolicyInput{}, err
	}
	rollout, err := rolloutStrategyFromProto(request.GetRolloutStrategy())
	if err != nil {
		return projectservice.PutReleasePolicyInput{}, err
	}
	rollback, err := rollbackPolicyFromProto(request.GetRollbackPolicy())
	if err != nil {
		return projectservice.PutReleasePolicyInput{}, err
	}
	status, err := releaseStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.PutReleasePolicyInput{}, err
	}
	return projectservice.PutReleasePolicyInput{ReleasePolicyID: id, ProjectID: projectID, Name: strings.TrimSpace(request.GetName()), BranchPattern: strings.TrimSpace(request.GetBranchPattern()), RolloutStrategy: rollout, RollbackPolicy: rollback, RiskProfileRef: strings.TrimSpace(request.GetRiskProfileRef()), Status: status, Meta: meta}, nil
}

func GetReleasePolicyInput(request *projectsv1.GetReleasePolicyRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetReleasePolicyId(), request.GetMeta())
}

func ListReleasePoliciesInput(request *projectsv1.ListReleasePoliciesRequest) (projectservice.ListReleasePoliciesInput, error) {
	return listProjectStatusesInput(
		request.GetProjectId(),
		request.GetMeta(),
		request.GetPage(),
		request.GetStatuses(),
		releaseStatusesFromProto,
		buildListReleasePoliciesInput,
	)
}

func PutReleaseLineInput(request *projectsv1.PutReleaseLineRequest) (projectservice.PutReleaseLineInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.PutReleaseLineInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetReleaseLineId())
	if err != nil {
		return projectservice.PutReleaseLineInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.PutReleaseLineInput{}, err
	}
	policyID, err := requiredUUID(request.GetReleasePolicyId())
	if err != nil {
		return projectservice.PutReleaseLineInput{}, err
	}
	status, err := releaseStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.PutReleaseLineInput{}, err
	}
	return projectservice.PutReleaseLineInput{ReleaseLineID: id, ProjectID: projectID, ReleasePolicyID: policyID, Name: strings.TrimSpace(request.GetName()), BranchPattern: strings.TrimSpace(request.GetBranchPattern()), Status: status, Meta: meta}, nil
}

func GetReleaseLineInput(request *projectsv1.GetReleaseLineRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetReleaseLineId(), request.GetMeta())
}

func ListReleaseLinesInput(request *projectsv1.ListReleaseLinesRequest) (projectservice.ListReleaseLinesInput, error) {
	return listProjectOptionalIDStatusesInput(
		request.GetProjectId(),
		request.GetReleasePolicyId(),
		request.GetMeta(),
		request.GetPage(),
		request.GetStatuses(),
		releaseStatusesFromProto,
		buildListReleaseLinesInput,
	)
}

func PutPlacementPolicyInput(request *projectsv1.PutPlacementPolicyRequest) (projectservice.PutPlacementPolicyInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return projectservice.PutPlacementPolicyInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetPlacementPolicyId())
	if err != nil {
		return projectservice.PutPlacementPolicyInput{}, err
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return projectservice.PutPlacementPolicyInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.PutPlacementPolicyInput{}, err
	}
	status, err := placementStatusFromProto(request.GetStatus())
	if err != nil {
		return projectservice.PutPlacementPolicyInput{}, err
	}
	return projectservice.PutPlacementPolicyInput{PlacementPolicyID: id, ProjectID: projectID, RepositoryID: repositoryID, ServiceKey: strings.TrimSpace(request.GetServiceKey()), AllowedClusterRefs: trimStrings(request.GetAllowedClusterRefs()), Status: status, Meta: meta}, nil
}

func GetPlacementPolicyInput(request *projectsv1.GetPlacementPolicyRequest) (uuid.UUID, value.QueryMeta, error) {
	return idWithQueryMeta(request.GetPlacementPolicyId(), request.GetMeta())
}

func ListPlacementPoliciesInput(request *projectsv1.ListPlacementPoliciesRequest) (projectservice.ListPlacementPoliciesInput, error) {
	projectID, meta, err := idWithQueryMeta(request.GetProjectId(), request.GetMeta())
	if err != nil {
		return projectservice.ListPlacementPoliciesInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return projectservice.ListPlacementPoliciesInput{}, err
	}
	statuses, err := placementStatusesFromProto(request.GetStatuses())
	if err != nil {
		return projectservice.ListPlacementPoliciesInput{}, err
	}
	return projectservice.ListPlacementPoliciesInput{ProjectID: projectID, RepositoryID: repositoryID, ServiceKey: strings.TrimSpace(request.GetServiceKey()), Statuses: statuses, Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

func serviceDescriptorsFromProto(items []*projectsv1.ServiceDescriptor) ([]entity.ServiceDescriptor, error) {
	result := make([]entity.ServiceDescriptor, 0, len(items))
	for _, item := range items {
		kind, err := serviceKindFromProto(item.GetKind())
		if err != nil {
			return nil, err
		}
		status, err := serviceStatusFromProto(item.GetStatus())
		if err != nil {
			return nil, err
		}
		repositoryID, err := optionalUUIDPtr(item.GetRepositoryId())
		if err != nil {
			return nil, err
		}
		result = append(result, entity.ServiceDescriptor{
			RepositoryID:         repositoryID,
			ServiceKey:           strings.TrimSpace(item.GetServiceKey()),
			DisplayName:          strings.TrimSpace(item.GetDisplayName()),
			Kind:                 kind,
			RootPath:             strings.TrimSpace(item.GetRootPath()),
			DocumentationScopeID: strings.TrimSpace(item.GetDocumentationScopeId()),
			DependsOnServiceKeys: trimStrings(item.GetDependsOnServiceKeys()),
			Status:               status,
		})
	}
	return result, nil
}

func projectStatusesFromProto(values []projectsv1.ProjectStatus) ([]enum.ProjectStatus, error) {
	return enumSlice(values, projectStatusFromProto)
}

func repositoryStatusesFromProto(values []projectsv1.RepositoryStatus) ([]enum.RepositoryStatus, error) {
	return enumSlice(values, repositoryStatusFromProto)
}

func serviceStatusesFromProto(values []projectsv1.ServiceStatus) ([]enum.ServiceStatus, error) {
	return enumSlice(values, serviceStatusFromProto)
}

func documentationStatusesFromProto(values []projectsv1.DocumentationSourceStatus) ([]enum.DocumentationSourceStatus, error) {
	return enumSlice(values, documentationStatusFromProto)
}

func branchRulesStatusesFromProto(values []projectsv1.BranchRulesStatus) ([]enum.BranchRulesStatus, error) {
	return enumSlice(values, branchRulesStatusFromProto)
}

func releaseStatusesFromProto(values []projectsv1.ReleasePolicyStatus) ([]enum.ReleasePolicyStatus, error) {
	return enumSlice(values, releaseStatusFromProto)
}

func placementStatusesFromProto(values []projectsv1.PlacementPolicyStatus) ([]enum.PlacementPolicyStatus, error) {
	return enumSlice(values, placementStatusFromProto)
}

func policyOverrideTargetsFromProto(values []projectsv1.PolicyOverrideTargetType) ([]enum.PolicyOverrideTargetType, error) {
	return enumSlice(values, policyOverrideTargetFromProto)
}

func policyOverrideStatusesFromProto(values []projectsv1.PolicyOverrideStatus) ([]enum.PolicyOverrideStatus, error) {
	return enumSlice(values, policyOverrideStatusFromProto)
}

func enumSlice[P comparable, D comparable](values []P, cast func(P) (D, error)) ([]D, error) {
	result := make([]D, 0, len(values))
	var zero D
	for _, value := range values {
		casted, err := cast(value)
		if err != nil {
			return nil, err
		}
		if casted != zero {
			result = append(result, casted)
		}
	}
	return result, nil
}

func requiredUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		id, err := requiredUUID(value)
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func trimStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func idWithQueryMeta(rawID string, rawMeta *projectsv1.QueryMeta) (uuid.UUID, value.QueryMeta, error) {
	return idWithMeta(rawID, rawMeta, QueryMetaFromProto)
}

func idWithCommandMeta(rawID string, rawMeta *projectsv1.CommandMeta) (uuid.UUID, value.CommandMeta, error) {
	return idWithMeta(rawID, rawMeta, CommandMetaFromProto)
}

func idWithMeta[Meta any, ProtoMeta any](rawID string, rawMeta ProtoMeta, castMeta func(ProtoMeta) (Meta, error)) (uuid.UUID, Meta, error) {
	id, err := requiredUUID(rawID)
	if err != nil {
		var zero Meta
		return uuid.Nil, zero, err
	}
	meta, err := castMeta(rawMeta)
	return id, meta, err
}

func listProjectStatusesInput[ProtoStatus comparable, DomainStatus comparable, Result any](
	rawProjectID string,
	rawMeta *projectsv1.QueryMeta,
	rawPage *projectsv1.PageRequest,
	rawStatuses []ProtoStatus,
	castStatuses func([]ProtoStatus) ([]DomainStatus, error),
	build func(uuid.UUID, []DomainStatus, value.PageRequest, value.QueryMeta) Result,
) (Result, error) {
	var zero Result
	projectID, meta, err := idWithQueryMeta(rawProjectID, rawMeta)
	if err != nil {
		return zero, err
	}
	statuses, err := castStatuses(rawStatuses)
	if err != nil {
		return zero, err
	}
	return build(projectID, statuses, pageRequestFromProto(rawPage), meta), nil
}

func listProjectOptionalIDStatusesInput[ProtoStatus comparable, DomainStatus comparable, Result any](
	rawProjectID string,
	rawOptionalID string,
	rawMeta *projectsv1.QueryMeta,
	rawPage *projectsv1.PageRequest,
	rawStatuses []ProtoStatus,
	castStatuses func([]ProtoStatus) ([]DomainStatus, error),
	build func(uuid.UUID, *uuid.UUID, []DomainStatus, value.PageRequest, value.QueryMeta) Result,
) (Result, error) {
	var zero Result
	projectID, meta, err := idWithQueryMeta(rawProjectID, rawMeta)
	if err != nil {
		return zero, err
	}
	optionalID, err := optionalUUIDPtr(rawOptionalID)
	if err != nil {
		return zero, err
	}
	statuses, err := castStatuses(rawStatuses)
	if err != nil {
		return zero, err
	}
	return build(projectID, optionalID, statuses, pageRequestFromProto(rawPage), meta), nil
}

func buildListRepositoriesInput(projectID uuid.UUID, statuses []enum.RepositoryStatus, page value.PageRequest, meta value.QueryMeta) projectservice.ListRepositoriesInput {
	return projectservice.ListRepositoriesInput{ProjectID: projectID, Statuses: statuses, Page: page, Meta: meta}
}

func buildListReleasePoliciesInput(projectID uuid.UUID, statuses []enum.ReleasePolicyStatus, page value.PageRequest, meta value.QueryMeta) projectservice.ListReleasePoliciesInput {
	return projectservice.ListReleasePoliciesInput{ProjectID: projectID, Statuses: statuses, Page: page, Meta: meta}
}

func buildListBranchRulesInput(projectID uuid.UUID, repositoryID *uuid.UUID, statuses []enum.BranchRulesStatus, page value.PageRequest, meta value.QueryMeta) projectservice.ListBranchRulesInput {
	return projectservice.ListBranchRulesInput{ProjectID: projectID, RepositoryID: repositoryID, Statuses: statuses, Page: page, Meta: meta}
}

func buildListReleaseLinesInput(projectID uuid.UUID, policyID *uuid.UUID, statuses []enum.ReleasePolicyStatus, page value.PageRequest, meta value.QueryMeta) projectservice.ListReleaseLinesInput {
	input := projectservice.ListReleaseLinesInput{ProjectID: projectID, ReleasePolicyID: policyID}
	input.Statuses = statuses
	input.Page = page
	input.Meta = meta
	return input
}
