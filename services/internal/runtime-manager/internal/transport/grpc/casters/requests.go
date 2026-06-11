package casters

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// PrepareRuntimeInput maps a gRPC prepare request to the facade use-case input.
func PrepareRuntimeInput(request *runtimev1.PrepareRuntimeRequest) (runtimeservice.PrepareRuntimeInput, error) {
	if request == nil {
		return runtimeservice.PrepareRuntimeInput{}, errs.ErrInvalidArgument
	}
	runtimeMode, err := RuntimeModeFromProto(request.GetRuntimeMode())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	agentRunID, err := optionalUUIDPtr(request.GetAgentRunId())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	policy, err := WorkspacePolicyInputFromProto(request.GetWorkspacePolicy())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	constraints, err := PlacementConstraintsInputFromProto(request.GetPlacementConstraints())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	return runtimeservice.PrepareRuntimeInput{
		AgentRunID:           agentRunID,
		RuntimeProfile:       strings.TrimSpace(request.GetRuntimeProfile()),
		RuntimeMode:          runtimeMode,
		WorkspacePolicy:      policy,
		PlacementConstraints: constraints,
		Meta:                 meta,
	}, nil
}

// ReserveSlotInput maps a gRPC reserve request to the use-case input.
func ReserveSlotInput(request *runtimev1.ReserveSlotRequest) (runtimeservice.ReserveSlotInput, error) {
	if request == nil {
		return runtimeservice.ReserveSlotInput{}, errs.ErrInvalidArgument
	}
	runtimeMode, err := RuntimeModeFromProto(request.GetRuntimeMode())
	if err != nil {
		return runtimeservice.ReserveSlotInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ReserveSlotInput{}, err
	}
	repositoryIDs, err := repeatedUUIDs(request.GetRepositoryIds())
	if err != nil {
		return runtimeservice.ReserveSlotInput{}, err
	}
	agentRunID, err := optionalUUIDPtr(request.GetAgentRunId())
	if err != nil {
		return runtimeservice.ReserveSlotInput{}, err
	}
	projectID, err := optionalUUIDPtr(request.GetProjectId())
	if err != nil {
		return runtimeservice.ReserveSlotInput{}, err
	}
	constraints, err := PlacementConstraintsInputFromProto(request.GetPlacementConstraints())
	if err != nil {
		return runtimeservice.ReserveSlotInput{}, err
	}
	return runtimeservice.ReserveSlotInput{
		RuntimeProfile:        strings.TrimSpace(request.GetRuntimeProfile()),
		RuntimeMode:           runtimeMode,
		WorkspacePolicyDigest: strings.TrimSpace(request.GetWorkspacePolicyDigest()),
		AgentRunID:            agentRunID,
		ProjectID:             projectID,
		RepositoryIDs:         repositoryIDs,
		PlacementConstraints:  constraints,
		Meta:                  meta,
	}, nil
}

// StartWorkspaceMaterializationInput maps a gRPC workspace start request.
func StartWorkspaceMaterializationInput(request *runtimev1.StartWorkspaceMaterializationRequest) (runtimeservice.StartWorkspaceMaterializationInput, error) {
	if request == nil {
		return runtimeservice.StartWorkspaceMaterializationInput{}, errs.ErrInvalidArgument
	}
	slotID, err := requiredUUID(request.GetSlotId())
	if err != nil {
		return runtimeservice.StartWorkspaceMaterializationInput{}, err
	}
	policy, err := WorkspacePolicyInputFromProto(request.GetWorkspacePolicy())
	if err != nil {
		return runtimeservice.StartWorkspaceMaterializationInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.StartWorkspaceMaterializationInput{}, err
	}
	return runtimeservice.StartWorkspaceMaterializationInput{SlotID: slotID, WorkspacePolicy: policy, Meta: meta}, nil
}

// ReportWorkspaceMaterializationProgressInput maps a gRPC workspace progress request.
func ReportWorkspaceMaterializationProgressInput(request *runtimev1.ReportWorkspaceMaterializationProgressRequest) (runtimeservice.ReportWorkspaceMaterializationProgressInput, error) {
	if request == nil {
		return runtimeservice.ReportWorkspaceMaterializationProgressInput{}, errs.ErrInvalidArgument
	}
	workspaceID, err := requiredUUID(request.GetWorkspaceMaterializationId())
	if err != nil {
		return runtimeservice.ReportWorkspaceMaterializationProgressInput{}, err
	}
	status, err := WorkspaceMaterializationStatusFromProto(request.GetStatus())
	if err != nil {
		return runtimeservice.ReportWorkspaceMaterializationProgressInput{}, err
	}
	startedAt, err := optionalTime(request.GetStartedAt())
	if err != nil {
		return runtimeservice.ReportWorkspaceMaterializationProgressInput{}, err
	}
	finishedAt, err := optionalTime(request.GetFinishedAt())
	if err != nil {
		return runtimeservice.ReportWorkspaceMaterializationProgressInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ReportWorkspaceMaterializationProgressInput{}, err
	}
	return runtimeservice.ReportWorkspaceMaterializationProgressInput{
		WorkspaceMaterializationID: workspaceID,
		Status:                     status,
		Fingerprint:                strings.TrimSpace(request.GetFingerprint()),
		StartedAt:                  startedAt,
		FinishedAt:                 finishedAt,
		ErrorCode:                  strings.TrimSpace(request.GetErrorCode()),
		ErrorMessage:               strings.TrimSpace(request.GetErrorMessage()),
		Meta:                       meta,
	}, nil
}

// GetWorkspaceMaterializationInput maps a get request into id and query metadata.
func GetWorkspaceMaterializationInput(request *runtimev1.GetWorkspaceMaterializationRequest) (runtimeservice.GetWorkspaceMaterializationInput, error) {
	if request == nil {
		return runtimeservice.GetWorkspaceMaterializationInput{}, errs.ErrInvalidArgument
	}
	workspaceID, meta, err := requiredIDAndQueryMeta(request.GetWorkspaceMaterializationId(), request.GetMeta())
	if err != nil {
		return runtimeservice.GetWorkspaceMaterializationInput{}, err
	}
	result := runtimeservice.GetWorkspaceMaterializationInput{}
	result.WorkspaceMaterializationID = workspaceID
	result.Meta = meta
	return result, nil
}

// ListWorkspaceMaterializationsInput maps materialization list filters.
func ListWorkspaceMaterializationsInput(request *runtimev1.ListWorkspaceMaterializationsRequest) (runtimeservice.ListWorkspaceMaterializationsInput, error) {
	if request == nil {
		return runtimeservice.ListWorkspaceMaterializationsInput{}, errs.ErrInvalidArgument
	}
	slotID, err := optionalUUIDPtr(request.GetSlotId())
	if err != nil {
		return runtimeservice.ListWorkspaceMaterializationsInput{}, err
	}
	agentRunID, err := optionalUUIDPtr(request.GetAgentRunId())
	if err != nil {
		return runtimeservice.ListWorkspaceMaterializationsInput{}, err
	}
	statuses, err := workspaceMaterializationStatusesFromProto(request.GetStatuses())
	if err != nil {
		return runtimeservice.ListWorkspaceMaterializationsInput{}, err
	}
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ListWorkspaceMaterializationsInput{}, err
	}
	return runtimeservice.ListWorkspaceMaterializationsInput{
		SlotID:     slotID,
		AgentRunID: agentRunID,
		Statuses:   statuses,
		Page:       pageRequestFromProto(request.GetPage()),
		Meta:       meta,
	}, nil
}

// PrepareBuildContextInput maps a gRPC build context request.
func PrepareBuildContextInput(request *runtimev1.PrepareBuildContextRequest) (runtimeservice.PrepareBuildContextInput, error) {
	if request == nil {
		return runtimeservice.PrepareBuildContextInput{}, errs.ErrInvalidArgument
	}
	projectID, err := requiredUUID(request.GetProjectId())
	if err != nil {
		return runtimeservice.PrepareBuildContextInput{}, err
	}
	repositoryID, err := requiredUUID(request.GetRepositoryId())
	if err != nil {
		return runtimeservice.PrepareBuildContextInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.PrepareBuildContextInput{}, err
	}
	return runtimeservice.PrepareBuildContextInput{
		ProjectID:            projectID,
		RepositoryID:         repositoryID,
		Provider:             strings.TrimSpace(request.GetProvider()),
		ProviderOwner:        strings.TrimSpace(request.GetProviderOwner()),
		ProviderName:         strings.TrimSpace(request.GetProviderName()),
		SourceRef:            strings.TrimSpace(request.GetSourceRef()),
		SourceCommitSHA:      strings.TrimSpace(request.GetSourceCommitSha()),
		AffectedServiceKeys:  append([]string(nil), request.GetAffectedServiceKeys()...),
		BuildPlanFingerprint: strings.TrimSpace(request.GetBuildPlanFingerprint()),
		SourceSnapshotRef:    strings.TrimSpace(request.GetSourceSnapshotRef()),
		SourceSnapshotDigest: strings.TrimSpace(request.GetSourceSnapshotDigest()),
		Meta:                 meta,
	}, nil
}

// ReportBuildContextProgressInput maps a gRPC build context progress request.
func ReportBuildContextProgressInput(request *runtimev1.ReportBuildContextProgressRequest) (runtimeservice.ReportBuildContextProgressInput, error) {
	if request == nil {
		return runtimeservice.ReportBuildContextProgressInput{}, errs.ErrInvalidArgument
	}
	buildContextID, err := requiredUUID(request.GetBuildContextId())
	if err != nil {
		return runtimeservice.ReportBuildContextProgressInput{}, err
	}
	status, err := BuildContextStatusFromProto(request.GetStatus())
	if err != nil {
		return runtimeservice.ReportBuildContextProgressInput{}, err
	}
	startedAt, err := optionalTime(request.GetStartedAt())
	if err != nil {
		return runtimeservice.ReportBuildContextProgressInput{}, err
	}
	finishedAt, err := optionalTime(request.GetFinishedAt())
	if err != nil {
		return runtimeservice.ReportBuildContextProgressInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ReportBuildContextProgressInput{}, err
	}
	return runtimeservice.ReportBuildContextProgressInput{
		BuildContextID:       buildContextID,
		Status:               status,
		SourceSnapshotRef:    strings.TrimSpace(request.GetSourceSnapshotRef()),
		SourceSnapshotDigest: strings.TrimSpace(request.GetSourceSnapshotDigest()),
		BuildContextRef:      strings.TrimSpace(request.GetBuildContextRef()),
		BuildContextDigest:   strings.TrimSpace(request.GetBuildContextDigest()),
		StartedAt:            startedAt,
		FinishedAt:           finishedAt,
		ErrorCode:            strings.TrimSpace(request.GetErrorCode()),
		ErrorMessage:         strings.TrimSpace(request.GetErrorMessage()),
		NextAction:           strings.TrimSpace(request.GetNextAction()),
		Meta:                 meta,
	}, nil
}

// GetBuildContextInput maps a build context read request.
func GetBuildContextInput(request *runtimev1.GetBuildContextRequest) (runtimeservice.GetBuildContextInput, error) {
	if request == nil {
		return runtimeservice.GetBuildContextInput{}, errs.ErrInvalidArgument
	}
	input := runtimeservice.GetBuildContextInput{
		ContextFingerprint: strings.TrimSpace(request.GetContextFingerprint()),
	}
	if err := fillBuildContextReadInput(request, &input); err != nil {
		return runtimeservice.GetBuildContextInput{}, err
	}
	return input, nil
}

func fillBuildContextReadInput(request *runtimev1.GetBuildContextRequest, input *runtimeservice.GetBuildContextInput) error {
	buildContextID, err := optionalUUIDValue(request.GetBuildContextId())
	if err != nil {
		return err
	}
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return err
	}
	input.BuildContextID = buildContextID
	input.Meta = meta
	return nil
}

// WorkspacePolicyInputFromProto maps a checked workspace policy to the domain model.
func WorkspacePolicyInputFromProto(policy *runtimev1.WorkspacePolicyInput) (runtimeservice.WorkspacePolicyInput, error) {
	if policy == nil {
		return runtimeservice.WorkspacePolicyInput{}, errs.ErrInvalidArgument
	}
	projectID, err := requiredUUID(policy.GetProjectId())
	if err != nil {
		return runtimeservice.WorkspacePolicyInput{}, err
	}
	sources, err := workspaceSourcesFromProto(policy.GetSources())
	if err != nil {
		return runtimeservice.WorkspacePolicyInput{}, err
	}
	return runtimeservice.WorkspacePolicyInput{
		ProjectID:               projectID,
		PolicyDigest:            strings.TrimSpace(policy.GetPolicyDigest()),
		PolicyVersion:           policy.GetPolicyVersion(),
		Sources:                 sources,
		ActivePolicyOverrideIDs: append([]string(nil), policy.GetActivePolicyOverrideIds()...),
	}, nil
}

// ExtendSlotLeaseInput maps a gRPC lease extension request.
func ExtendSlotLeaseInput(request *runtimev1.ExtendSlotLeaseRequest) (runtimeservice.ExtendSlotLeaseInput, error) {
	if request == nil {
		return runtimeservice.ExtendSlotLeaseInput{}, errs.ErrInvalidArgument
	}
	slotID, err := requiredUUID(request.GetSlotId())
	if err != nil {
		return runtimeservice.ExtendSlotLeaseInput{}, err
	}
	lease, err := commandLeaseFromProto(request.GetLeaseOwner(), request.GetLeaseUntil(), request.GetMeta())
	if err != nil {
		return runtimeservice.ExtendSlotLeaseInput{}, err
	}
	input := runtimeservice.ExtendSlotLeaseInput{SlotID: slotID}
	input.LeaseOwner = lease.Owner
	input.LeaseUntil = lease.Until
	input.Meta = lease.Meta
	return input, nil
}

// ReleaseSlotInput maps a gRPC slot release request.
func ReleaseSlotInput(request *runtimev1.ReleaseSlotRequest) (runtimeservice.ReleaseSlotInput, error) {
	if request == nil {
		return runtimeservice.ReleaseSlotInput{}, errs.ErrInvalidArgument
	}
	slotID, err := requiredUUID(request.GetSlotId())
	if err != nil {
		return runtimeservice.ReleaseSlotInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ReleaseSlotInput{}, err
	}
	return runtimeservice.ReleaseSlotInput{SlotID: slotID, LeaseOwner: strings.TrimSpace(request.GetLeaseOwner()), Meta: meta}, nil
}

// MarkSlotFailedInput maps a gRPC slot failure request.
func MarkSlotFailedInput(request *runtimev1.MarkSlotFailedRequest) (runtimeservice.MarkSlotFailedInput, error) {
	if request == nil {
		return runtimeservice.MarkSlotFailedInput{}, errs.ErrInvalidArgument
	}
	slotID, err := requiredUUID(request.GetSlotId())
	if err != nil {
		return runtimeservice.MarkSlotFailedInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.MarkSlotFailedInput{}, err
	}
	return runtimeservice.MarkSlotFailedInput{
		SlotID:       slotID,
		ErrorCode:    strings.TrimSpace(request.GetErrorCode()),
		ErrorMessage: strings.TrimSpace(request.GetErrorMessage()),
		Meta:         meta,
	}, nil
}

// GetSlotInput maps a get request into id and query metadata.
func GetSlotInput(request *runtimev1.GetSlotRequest) (runtimeservice.GetSlotInput, error) {
	if request == nil {
		return runtimeservice.GetSlotInput{}, errs.ErrInvalidArgument
	}
	slotID, meta, err := requiredIDAndQueryMeta(request.GetSlotId(), request.GetMeta())
	if err != nil {
		return runtimeservice.GetSlotInput{}, err
	}
	return runtimeservice.GetSlotInput{SlotID: slotID, Meta: meta}, nil
}

// ListSlotsInput maps slot list filters.
func ListSlotsInput(request *runtimev1.ListSlotsRequest) (runtimeservice.ListSlotsInput, error) {
	if request == nil {
		return runtimeservice.ListSlotsInput{}, errs.ErrInvalidArgument
	}
	projectID, err := optionalUUIDPtr(request.GetProjectId())
	if err != nil {
		return runtimeservice.ListSlotsInput{}, err
	}
	fleetScopeID, err := optionalUUIDPtr(request.GetFleetScopeId())
	if err != nil {
		return runtimeservice.ListSlotsInput{}, err
	}
	agentRunID, err := optionalUUIDPtr(request.GetAgentRunId())
	if err != nil {
		return runtimeservice.ListSlotsInput{}, err
	}
	statuses, err := slotStatusesFromProto(request.GetStatuses())
	if err != nil {
		return runtimeservice.ListSlotsInput{}, err
	}
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ListSlotsInput{}, err
	}
	return runtimeservice.ListSlotsInput{
		ProjectID:      projectID,
		Statuses:       statuses,
		RuntimeProfile: strings.TrimSpace(request.GetRuntimeProfile()),
		FleetScopeID:   fleetScopeID,
		AgentRunID:     agentRunID,
		Page:           pageRequestFromProto(request.GetPage()),
		Meta:           meta,
	}, nil
}

// CreateJobInput maps a gRPC job creation request.
func CreateJobInput(request *runtimev1.CreateJobRequest) (runtimeservice.CreateJobInput, error) {
	if request == nil {
		return runtimeservice.CreateJobInput{}, errs.ErrInvalidArgument
	}
	jobType, err := JobTypeFromProto(request.GetJobType())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	priority, err := JobPriorityFromProto(request.GetPriority())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	slotID, err := optionalUUIDPtr(request.GetSlotId())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	agentRunID, err := optionalUUIDPtr(request.GetAgentRunId())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	projectID, err := optionalUUIDPtr(request.GetProjectId())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	repositoryID, err := optionalUUIDPtr(request.GetRepositoryId())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	releaseLineID, err := optionalUUIDPtr(request.GetReleaseLineId())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	packageInstallationID, err := optionalUUIDPtr(request.GetPackageInstallationId())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	constraints, err := PlacementConstraintsInputFromProto(request.GetPlacementConstraints())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	agentRunExecutionSpec, err := AgentRunExecutionSpecInputFromProto(request.GetAgentRunExecutionSpec())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	buildExecutionSpec, err := BuildExecutionSpecInputFromProto(request.GetBuildExecutionSpec())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	deployExecutionSpec, err := DeployExecutionSpecInputFromProto(request.GetDeployExecutionSpec())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.CreateJobInput{}, err
	}
	return runtimeservice.CreateJobInput{
		JobType:               jobType,
		Priority:              priority,
		SlotID:                slotID,
		AgentRunID:            agentRunID,
		ProjectID:             projectID,
		RepositoryID:          repositoryID,
		ReleaseLineID:         releaseLineID,
		PackageInstallationID: packageInstallationID,
		PlacementConstraints:  constraints,
		JobInputJSON:          []byte(strings.TrimSpace(request.GetJobInputJson())),
		AgentRunExecutionSpec: agentRunExecutionSpec,
		BuildExecutionSpec:    buildExecutionSpec,
		DeployExecutionSpec:   deployExecutionSpec,
		Meta:                  meta,
	}, nil
}

// BuildExecutionSpecInputFromProto maps typed build execution input from proto.
func BuildExecutionSpecInputFromProto(spec *runtimev1.BuildExecutionSpec) (*runtimeservice.BuildExecutionSpecInput, error) {
	if spec == nil {
		return nil, nil
	}
	return &runtimeservice.BuildExecutionSpecInput{
		SourceRef:            strings.TrimSpace(spec.GetSourceRef()),
		SourceCommitSHA:      strings.TrimSpace(spec.GetSourceCommitSha()),
		ServiceKey:           strings.TrimSpace(spec.GetServiceKey()),
		ImageRef:             strings.TrimSpace(spec.GetImageRef()),
		ImageTag:             strings.TrimSpace(spec.GetImageTag()),
		ImageDigest:          strings.TrimSpace(spec.GetImageDigest()),
		BuildContextRef:      strings.TrimSpace(spec.GetBuildContextRef()),
		BuildContextDigest:   strings.TrimSpace(spec.GetBuildContextDigest()),
		DockerfileRef:        strings.TrimSpace(spec.GetDockerfileRef()),
		DockerfileDigest:     strings.TrimSpace(spec.GetDockerfileDigest()),
		DockerfileTarget:     strings.TrimSpace(spec.GetDockerfileTarget()),
		BuilderImageRef:      strings.TrimSpace(spec.GetBuilderImageRef()),
		BuildPlanFingerprint: strings.TrimSpace(spec.GetBuildPlanFingerprint()),
		AllowedSecretRefs:    runtimeJobAllowedSecretRefsFromProto(spec.GetAllowedSecretRefs()),
		OutputRefs:           runtimeJobOutputRefsFromProto(spec.GetOutputRefs()),
	}, nil
}

// DeployExecutionSpecInputFromProto maps typed deploy execution input from proto.
func DeployExecutionSpecInputFromProto(spec *runtimev1.DeployExecutionSpec) (*runtimeservice.DeployExecutionSpecInput, error) {
	if spec == nil {
		return nil, nil
	}
	return &runtimeservice.DeployExecutionSpecInput{
		SourceRef:             strings.TrimSpace(spec.GetSourceRef()),
		SourceCommitSHA:       strings.TrimSpace(spec.GetSourceCommitSha()),
		ServiceKey:            strings.TrimSpace(spec.GetServiceKey()),
		ImageRef:              strings.TrimSpace(spec.GetImageRef()),
		ImageTag:              strings.TrimSpace(spec.GetImageTag()),
		ImageDigest:           strings.TrimSpace(spec.GetImageDigest()),
		ManifestRef:           strings.TrimSpace(spec.GetManifestRef()),
		ManifestDigest:        strings.TrimSpace(spec.GetManifestDigest()),
		KustomizationRef:      strings.TrimSpace(spec.GetKustomizationRef()),
		KustomizationDigest:   strings.TrimSpace(spec.GetKustomizationDigest()),
		TargetNamespace:       strings.TrimSpace(spec.GetTargetNamespace()),
		TargetClusterRef:      strings.TrimSpace(spec.GetTargetClusterRef()),
		TargetSlotID:          strings.TrimSpace(spec.GetTargetSlotId()),
		DeployPlanFingerprint: strings.TrimSpace(spec.GetDeployPlanFingerprint()),
		AllowedSecretRefs:     runtimeJobAllowedSecretRefsFromProto(spec.GetAllowedSecretRefs()),
		OutputRefs:            runtimeJobOutputRefsFromProto(spec.GetOutputRefs()),
	}, nil
}

func runtimeJobAllowedSecretRefsFromProto(refs []*runtimev1.RuntimeJobAllowedSecretRef) []runtimeservice.RuntimeJobExecutionRefInput {
	return agentRunProtoRefs(refs, (*runtimev1.RuntimeJobAllowedSecretRef).GetPurpose, (*runtimev1.RuntimeJobAllowedSecretRef).GetSecretRef)
}

func runtimeJobOutputRefsFromProto(refs []*runtimev1.RuntimeJobOutputRef) []runtimeservice.RuntimeJobExecutionRefInput {
	return agentRunProtoRefs(refs, (*runtimev1.RuntimeJobOutputRef).GetKind, (*runtimev1.RuntimeJobOutputRef).GetRef)
}

// AgentRunExecutionSpecInputFromProto maps typed agent_run execution input from proto.
func AgentRunExecutionSpecInputFromProto(spec *runtimev1.AgentRunExecutionSpec) (*runtimeservice.AgentRunExecutionSpecInput, error) {
	if spec == nil {
		return nil, nil
	}
	agentRunID, err := requiredUUID(spec.GetAgentRunId())
	if err != nil {
		return nil, err
	}
	slotID, err := requiredUUID(spec.GetSlotId())
	if err != nil {
		return nil, err
	}
	materializationID, err := requiredUUID(spec.GetExpectedMaterializationId())
	if err != nil {
		return nil, err
	}
	runnerMode, err := AgentRunRunnerModeFromProto(spec.GetRunnerMode())
	if err != nil {
		return nil, err
	}
	return &runtimeservice.AgentRunExecutionSpecInput{
		AgentRunID:                         agentRunID,
		SlotID:                             slotID,
		ExpectedMaterializationID:          materializationID,
		ExpectedMaterializationFingerprint: strings.TrimSpace(spec.GetExpectedMaterializationFingerprint()),
		WorkspaceRef:                       strings.TrimSpace(spec.GetWorkspaceRef()),
		WorkspaceMountRef:                  strings.TrimSpace(spec.GetWorkspaceMountRef()),
		WorkspacePVCRef:                    strings.TrimSpace(spec.GetWorkspacePvcRef()),
		ContextRef:                         strings.TrimSpace(spec.GetContextRef()),
		ContextDigest:                      strings.TrimSpace(spec.GetContextDigest()),
		RunnerProfileRef:                   strings.TrimSpace(spec.GetRunnerProfileRef()),
		RunnerImageRef:                     strings.TrimSpace(spec.GetRunnerImageRef()),
		RunnerMode:                         runnerMode,
		AllowedSecretRefs:                  agentRunAllowedSecretRefsFromProto(spec.GetAllowedSecretRefs()),
		ReportingTargetRefs:                agentRunReportingTargetRefsFromProto(spec.GetReportingTargetRefs()),
		CodexSessionExecutionSpec:          codexSessionExecutionSpecFromProto(spec.GetCodexSessionExecutionSpec()),
	}, nil
}

func agentRunAllowedSecretRefsFromProto(refs []*runtimev1.AgentRunAllowedSecretRef) []runtimeservice.AgentRunExecutionRefInput {
	return agentRunProtoRefs(refs, (*runtimev1.AgentRunAllowedSecretRef).GetPurpose, (*runtimev1.AgentRunAllowedSecretRef).GetSecretRef)
}

func agentRunReportingTargetRefsFromProto(refs []*runtimev1.AgentRunReportingTargetRef) []runtimeservice.AgentRunExecutionRefInput {
	return agentRunProtoRefs(refs, (*runtimev1.AgentRunReportingTargetRef).GetKind, (*runtimev1.AgentRunReportingTargetRef).GetRef)
}

func codexSessionExecutionSpecFromProto(spec *runtimev1.CodexSessionExecutionSpec) *runtimeservice.CodexSessionExecutionSpecInput {
	if spec == nil {
		return nil
	}
	return &runtimeservice.CodexSessionExecutionSpecInput{
		InstructionObjectRef:    strings.TrimSpace(spec.GetInstructionObjectRef()),
		InstructionObjectDigest: strings.TrimSpace(spec.GetInstructionObjectDigest()),
		ResultSchemaRef:         strings.TrimSpace(spec.GetResultSchemaRef()),
		ResultSchemaDigest:      strings.TrimSpace(spec.GetResultSchemaDigest()),
		SessionSnapshotRef:      strings.TrimSpace(spec.GetSessionSnapshotRef()),
		WorkspaceSnapshotRef:    strings.TrimSpace(spec.GetWorkspaceSnapshotRef()),
		HookEndpointRef:         strings.TrimSpace(spec.GetHookEndpointRef()),
		CallbackRefs:            agentRunExecutionRefsFromProto(spec.GetCallbackRefs()),
		TimeoutSeconds:          spec.GetTimeoutSeconds(),
		RunnerProfileRef:        strings.TrimSpace(spec.GetRunnerProfileRef()),
		RunnerMode:              mustAgentRunRunnerModeFromProto(spec.GetRunnerMode()),
		OutputRefs:              agentRunExecutionRefsFromProto(spec.GetOutputRefs()),
		ResultRefs:              agentRunExecutionRefsFromProto(spec.GetResultRefs()),
		AllowedSecretRefs:       agentRunAllowedSecretRefsFromProto(spec.GetAllowedSecretRefs()),
	}
}

func agentRunExecutionRefsFromProto(refs []*runtimev1.AgentRunExecutionRef) []runtimeservice.AgentRunExecutionRefInput {
	return agentRunProtoRefs(refs, (*runtimev1.AgentRunExecutionRef).GetKind, (*runtimev1.AgentRunExecutionRef).GetRef)
}

func mustAgentRunRunnerModeFromProto(value runtimev1.AgentRunRunnerMode) enum.AgentRunRunnerMode {
	mode, err := AgentRunRunnerModeFromProto(value)
	if err != nil {
		return ""
	}
	return mode
}

func agentRunProtoRefs[Proto any](refs []*Proto, kind func(*Proto) string, refValue func(*Proto) string) []runtimeservice.AgentRunExecutionRefInput {
	result := make([]runtimeservice.AgentRunExecutionRefInput, 0, len(refs))
	for _, ref := range refs {
		if ref != nil {
			result = append(result, runtimeservice.AgentRunExecutionRefInput{
				Kind: strings.TrimSpace(kind(ref)),
				Ref:  strings.TrimSpace(refValue(ref)),
			})
		}
	}
	return result
}

// ClaimRunnableJobInput maps a gRPC worker claim request.
func ClaimRunnableJobInput(request *runtimev1.ClaimRunnableJobRequest) (runtimeservice.ClaimRunnableJobInput, error) {
	if request == nil {
		return runtimeservice.ClaimRunnableJobInput{}, errs.ErrInvalidArgument
	}
	jobTypes, err := jobTypesFromProto(request.GetJobTypes())
	if err != nil {
		return runtimeservice.ClaimRunnableJobInput{}, err
	}
	leaseUntil, err := requiredTime(request.GetLeaseUntil())
	if err != nil {
		return runtimeservice.ClaimRunnableJobInput{}, err
	}
	fleetScopeID, err := optionalUUIDPtr(request.GetFleetScopeId())
	if err != nil {
		return runtimeservice.ClaimRunnableJobInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ClaimRunnableJobInput{}, err
	}
	return runtimeservice.ClaimRunnableJobInput{
		JobTypes:     jobTypes,
		WorkerID:     strings.TrimSpace(request.GetWorkerId()),
		LeaseOwner:   strings.TrimSpace(request.GetLeaseOwner()),
		LeaseUntil:   leaseUntil,
		FleetScopeID: fleetScopeID,
		Meta:         meta,
	}, nil
}

// ReportJobStepProgressInput maps a gRPC job step update.
func ReportJobStepProgressInput(request *runtimev1.ReportJobStepProgressRequest) (runtimeservice.ReportJobStepProgressInput, error) {
	if request == nil {
		return runtimeservice.ReportJobStepProgressInput{}, errs.ErrInvalidArgument
	}
	jobID, err := requiredUUID(request.GetJobId())
	if err != nil {
		return runtimeservice.ReportJobStepProgressInput{}, err
	}
	status, err := JobStepStatusFromProto(request.GetStatus())
	if err != nil {
		return runtimeservice.ReportJobStepProgressInput{}, err
	}
	startedAt, err := optionalTime(request.GetStartedAt())
	if err != nil {
		return runtimeservice.ReportJobStepProgressInput{}, err
	}
	finishedAt, err := optionalTime(request.GetFinishedAt())
	if err != nil {
		return runtimeservice.ReportJobStepProgressInput{}, err
	}
	refs, err := runtimeArtifactRefInputsFromProto(request.GetArtifactRefs())
	if err != nil {
		return runtimeservice.ReportJobStepProgressInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ReportJobStepProgressInput{}, err
	}
	return runtimeservice.ReportJobStepProgressInput{
		JobID:        jobID,
		LeaseToken:   strings.TrimSpace(request.GetLeaseToken()),
		StepKey:      strings.TrimSpace(request.GetStepKey()),
		Status:       status,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		ShortLogTail: request.GetShortLogTail(),
		ExternalRef:  strings.TrimSpace(request.GetExternalRef()),
		ErrorCode:    strings.TrimSpace(request.GetErrorCode()),
		ErrorMessage: strings.TrimSpace(request.GetErrorMessage()),
		ArtifactRefs: refs,
		Meta:         meta,
	}, nil
}

// CompleteJobInput maps a gRPC successful job completion.
func CompleteJobInput(request *runtimev1.CompleteJobRequest) (runtimeservice.CompleteJobInput, error) {
	if request == nil {
		return runtimeservice.CompleteJobInput{}, errs.ErrInvalidArgument
	}
	jobID, err := requiredUUID(request.GetJobId())
	if err != nil {
		return runtimeservice.CompleteJobInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.CompleteJobInput{}, err
	}
	return runtimeservice.CompleteJobInput{
		JobID:        jobID,
		LeaseToken:   strings.TrimSpace(request.GetLeaseToken()),
		ShortLogTail: request.GetShortLogTail(),
		FullLogRef:   strings.TrimSpace(request.GetFullLogRef()),
		Meta:         meta,
	}, nil
}

// FailJobInput maps a gRPC failed job completion.
func FailJobInput(request *runtimev1.FailJobRequest) (runtimeservice.FailJobInput, error) {
	if request == nil {
		return runtimeservice.FailJobInput{}, errs.ErrInvalidArgument
	}
	jobID, err := requiredUUID(request.GetJobId())
	if err != nil {
		return runtimeservice.FailJobInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.FailJobInput{}, err
	}
	return runtimeservice.FailJobInput{
		JobID:        jobID,
		LeaseToken:   strings.TrimSpace(request.GetLeaseToken()),
		ErrorCode:    strings.TrimSpace(request.GetErrorCode()),
		ErrorMessage: strings.TrimSpace(request.GetErrorMessage()),
		ShortLogTail: request.GetShortLogTail(),
		FullLogRef:   strings.TrimSpace(request.GetFullLogRef()),
		Meta:         meta,
	}, nil
}

// CancelJobInput maps a gRPC cancellation request.
func CancelJobInput(request *runtimev1.CancelJobRequest) (runtimeservice.CancelJobInput, error) {
	if request == nil {
		return runtimeservice.CancelJobInput{}, errs.ErrInvalidArgument
	}
	jobID, err := requiredUUID(request.GetJobId())
	if err != nil {
		return runtimeservice.CancelJobInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.CancelJobInput{}, err
	}
	return runtimeservice.CancelJobInput{JobID: jobID, Meta: meta}, nil
}

// GetJobInput maps a get request into id and query metadata.
func GetJobInput(request *runtimev1.GetJobRequest) (runtimeservice.GetJobInput, error) {
	result := runtimeservice.GetJobInput{}
	if request == nil {
		return result, errs.ErrInvalidArgument
	}
	jobID, err := requiredUUID(request.GetJobId())
	if err != nil {
		return result, err
	}
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return result, err
	}
	result.JobID = jobID
	result.Meta = meta
	return result, nil
}

// ListJobsInput maps job list filters.
func ListJobsInput(request *runtimev1.ListJobsRequest) (runtimeservice.ListJobsInput, error) {
	if request == nil {
		return runtimeservice.ListJobsInput{}, errs.ErrInvalidArgument
	}
	statuses, err := jobStatusesFromProto(request.GetStatuses())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	jobTypes, err := jobTypesFromProto(request.GetJobTypes())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	projectID, err := optionalUUIDPtr(request.GetProjectId())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	slotID, err := optionalUUIDPtr(request.GetSlotId())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	agentRunID, err := optionalUUIDPtr(request.GetAgentRunId())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	releaseLineID, err := optionalUUIDPtr(request.GetReleaseLineId())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ListJobsInput{}, err
	}
	return runtimeservice.ListJobsInput{
		Statuses:      statuses,
		JobTypes:      jobTypes,
		ProjectID:     projectID,
		SlotID:        slotID,
		AgentRunID:    agentRunID,
		ReleaseLineID: releaseLineID,
		Page:          pageRequestFromProto(request.GetPage()),
		Meta:          meta,
	}, nil
}

// RecordRuntimeArtifactRefInput maps one external artifact reference command.
func RecordRuntimeArtifactRefInput(request *runtimev1.RecordRuntimeArtifactRefRequest) (runtimeservice.RecordRuntimeArtifactRefInput, error) {
	if request == nil {
		return runtimeservice.RecordRuntimeArtifactRefInput{}, errs.ErrInvalidArgument
	}
	jobID, err := optionalUUIDPtr(request.GetJobId())
	if err != nil {
		return runtimeservice.RecordRuntimeArtifactRefInput{}, err
	}
	slotID, err := optionalUUIDPtr(request.GetSlotId())
	if err != nil {
		return runtimeservice.RecordRuntimeArtifactRefInput{}, err
	}
	ref, err := runtimeArtifactRefInputFromProto(request.GetArtifactRef())
	if err != nil {
		return runtimeservice.RecordRuntimeArtifactRefInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.RecordRuntimeArtifactRefInput{}, err
	}
	return runtimeservice.RecordRuntimeArtifactRefInput{JobID: jobID, SlotID: slotID, ArtifactRef: ref, Meta: meta}, nil
}

// ListRuntimeArtifactRefsInput maps artifact reference list filters.
func ListRuntimeArtifactRefsInput(request *runtimev1.ListRuntimeArtifactRefsRequest) (runtimeservice.ListRuntimeArtifactRefsInput, error) {
	if request == nil {
		return runtimeservice.ListRuntimeArtifactRefsInput{}, errs.ErrInvalidArgument
	}
	result := runtimeservice.ListRuntimeArtifactRefsInput{Page: pageRequestFromProto(request.GetPage())}
	jobID, err := optionalUUIDPtr(request.GetJobId())
	if err != nil {
		return runtimeservice.ListRuntimeArtifactRefsInput{}, err
	}
	result.JobID = jobID
	slotID, err := optionalUUIDPtr(request.GetSlotId())
	if err != nil {
		return runtimeservice.ListRuntimeArtifactRefsInput{}, err
	}
	result.SlotID = slotID
	artifactTypes, err := runtimeArtifactTypesFromProto(request.GetArtifactTypes())
	if err != nil {
		return runtimeservice.ListRuntimeArtifactRefsInput{}, err
	}
	result.ArtifactTypes = artifactTypes
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ListRuntimeArtifactRefsInput{}, err
	}
	result.Meta = meta
	return result, nil
}

// CreateOrUpdateCleanupPolicyInput maps a gRPC cleanup policy upsert request.
func CreateOrUpdateCleanupPolicyInput(request *runtimev1.CreateOrUpdateCleanupPolicyRequest) (runtimeservice.CreateOrUpdateCleanupPolicyInput, error) {
	if request == nil {
		return runtimeservice.CreateOrUpdateCleanupPolicyInput{}, errs.ErrInvalidArgument
	}
	scope, err := RuntimeScopeTypeFromProto(request.GetScopeType())
	if err != nil {
		return runtimeservice.CreateOrUpdateCleanupPolicyInput{}, err
	}
	status, err := CleanupPolicyStatusFromProto(request.GetStatus())
	if err != nil {
		return runtimeservice.CreateOrUpdateCleanupPolicyInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetCleanupPolicyId())
	if err != nil {
		return runtimeservice.CreateOrUpdateCleanupPolicyInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.CreateOrUpdateCleanupPolicyInput{}, err
	}
	return runtimeservice.CreateOrUpdateCleanupPolicyInput{
		CleanupPolicyID:  id,
		ScopeType:        scope,
		ScopeID:          strings.TrimSpace(request.GetScopeId()),
		TTLSeconds:       request.GetTtlSeconds(),
		FailedTTLSeconds: request.GetFailedTtlSeconds(),
		KeepShortLogTail: request.GetKeepShortLogTail(),
		Status:           status,
		Meta:             meta,
	}, nil
}

// RunCleanupBatchInput maps a gRPC cleanup batch request.
func RunCleanupBatchInput(request *runtimev1.RunCleanupBatchRequest) (runtimeservice.RunCleanupBatchInput, error) {
	if request == nil {
		return runtimeservice.RunCleanupBatchInput{}, errs.ErrInvalidArgument
	}
	id, err := optionalUUIDPtr(request.GetCleanupPolicyId())
	if err != nil {
		return runtimeservice.RunCleanupBatchInput{}, err
	}
	leaseUntil, err := requiredTime(request.GetLeaseUntil())
	if err != nil {
		return runtimeservice.RunCleanupBatchInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.RunCleanupBatchInput{}, err
	}
	return runtimeservice.RunCleanupBatchInput{
		CleanupPolicyID: id,
		Limit:           int(request.GetLimit()),
		LeaseOwner:      strings.TrimSpace(request.GetLeaseOwner()),
		LeaseUntil:      leaseUntil,
		Meta:            meta,
	}, nil
}

// CreateOrUpdatePrewarmPoolInput maps a gRPC prewarm pool upsert request.
func CreateOrUpdatePrewarmPoolInput(request *runtimev1.CreateOrUpdatePrewarmPoolRequest) (runtimeservice.CreateOrUpdatePrewarmPoolInput, error) {
	if request == nil {
		return runtimeservice.CreateOrUpdatePrewarmPoolInput{}, errs.ErrInvalidArgument
	}
	scope, err := PrewarmPoolScopeTypeFromProto(request.GetScopeType())
	if err != nil {
		return runtimeservice.CreateOrUpdatePrewarmPoolInput{}, err
	}
	status, err := PrewarmPoolStatusFromProto(request.GetStatus())
	if err != nil {
		return runtimeservice.CreateOrUpdatePrewarmPoolInput{}, err
	}
	id, err := optionalUUIDPtr(request.GetPrewarmPoolId())
	if err != nil {
		return runtimeservice.CreateOrUpdatePrewarmPoolInput{}, err
	}
	fleetScopeID, err := optionalUUIDPtr(request.GetFleetScopeId())
	if err != nil {
		return runtimeservice.CreateOrUpdatePrewarmPoolInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.CreateOrUpdatePrewarmPoolInput{}, err
	}
	return runtimeservice.CreateOrUpdatePrewarmPoolInput{
		PrewarmPoolID:  id,
		ScopeType:      scope,
		ScopeID:        strings.TrimSpace(request.GetScopeId()),
		RuntimeProfile: strings.TrimSpace(request.GetRuntimeProfile()),
		FleetScopeID:   fleetScopeID,
		TargetSize:     request.GetTargetSize(),
		Status:         status,
		Meta:           meta,
	}, nil
}

// ReconcilePrewarmPoolInput maps a gRPC prewarm reconciliation request.
func ReconcilePrewarmPoolInput(request *runtimev1.ReconcilePrewarmPoolRequest) (runtimeservice.ReconcilePrewarmPoolInput, error) {
	if request == nil {
		return runtimeservice.ReconcilePrewarmPoolInput{}, errs.ErrInvalidArgument
	}
	id, err := requiredUUID(request.GetPrewarmPoolId())
	if err != nil {
		return runtimeservice.ReconcilePrewarmPoolInput{}, err
	}
	lease, err := commandLeaseFromProto(request.GetLeaseOwner(), request.GetLeaseUntil(), request.GetMeta())
	if err != nil {
		return runtimeservice.ReconcilePrewarmPoolInput{}, err
	}
	return newPrewarmReconcileInput(id, lease), nil
}

func newPrewarmReconcileInput(id uuid.UUID, lease commandLease) runtimeservice.ReconcilePrewarmPoolInput {
	return runtimeservice.ReconcilePrewarmPoolInput{
		PrewarmPoolID: id,
		LeaseOwner:    lease.Owner,
		LeaseUntil:    lease.Until,
		Meta:          lease.Meta,
	}
}

type commandLease struct {
	Owner string
	Until time.Time
	Meta  value.CommandMeta
}

func commandLeaseFromProto(owner string, until string, metaProto *runtimev1.CommandMeta) (commandLease, error) {
	leaseUntil, err := requiredTime(until)
	if err != nil {
		return commandLease{}, err
	}
	meta, err := CommandMetaFromProto(metaProto)
	if err != nil {
		return commandLease{}, err
	}
	return commandLease{Owner: strings.TrimSpace(owner), Until: leaseUntil, Meta: meta}, nil
}

func repeatedUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, len(values))
	for index, value := range values {
		id, err := requiredUUID(value)
		if err != nil {
			return nil, err
		}
		result[index] = id
	}
	return result, nil
}

func requiredIDAndQueryMeta(idText string, metaProto *runtimev1.QueryMeta) (uuid.UUID, value.QueryMeta, error) {
	id, err := requiredUUID(idText)
	if err != nil {
		return uuid.Nil, value.QueryMeta{}, err
	}
	meta, err := QueryMetaFromProto(metaProto)
	if err != nil {
		return uuid.Nil, value.QueryMeta{}, err
	}
	return id, meta, nil
}

func workspaceSourcesFromProto(sources []*runtimev1.WorkspaceSource) ([]value.WorkspaceSource, error) {
	return repeatedEnumsFromProto(sources, workspaceSourceFromProto)
}

func workspaceSourceFromProto(source *runtimev1.WorkspaceSource) (value.WorkspaceSource, error) {
	if source == nil {
		return value.WorkspaceSource{}, errs.ErrInvalidArgument
	}
	kind, err := WorkspaceSourceKindFromProto(source.GetKind())
	if err != nil {
		return value.WorkspaceSource{}, err
	}
	accessMode, err := WorkspaceSourceAccessModeFromProto(source.GetAccessMode())
	if err != nil {
		return value.WorkspaceSource{}, err
	}
	repositoryID, err := optionalUUIDPtr(source.GetRepositoryId())
	if err != nil {
		return value.WorkspaceSource{}, err
	}
	metadata := json.RawMessage(source.GetMetadataJson())
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return value.WorkspaceSource{
		SourceID:      strings.TrimSpace(source.GetSourceId()),
		Kind:          kind,
		RepositoryID:  repositoryID,
		Provider:      strings.TrimSpace(source.GetProvider()),
		ProviderOwner: strings.TrimSpace(source.GetProviderOwner()),
		ProviderName:  strings.TrimSpace(source.GetProviderName()),
		SourceRef:     strings.TrimSpace(source.GetSourceRef()),
		CommitSHA:     strings.TrimSpace(source.GetCommitSha()),
		LocalPath:     strings.TrimSpace(source.GetLocalPath()),
		AccessMode:    accessMode,
		Digest:        strings.TrimSpace(source.GetDigest()),
		Metadata:      metadata,
	}, nil
}

func runtimeArtifactRefInputsFromProto(refs []*runtimev1.RuntimeArtifactRefInput) ([]runtimeservice.RuntimeArtifactRefInput, error) {
	return repeatedEnumsFromProto(refs, runtimeArtifactRefInputFromProto)
}

func runtimeArtifactRefInputFromProto(ref *runtimev1.RuntimeArtifactRefInput) (runtimeservice.RuntimeArtifactRefInput, error) {
	if ref == nil {
		return runtimeservice.RuntimeArtifactRefInput{}, errs.ErrInvalidArgument
	}
	artifactType, err := RuntimeArtifactTypeFromProto(ref.GetArtifactType())
	if err != nil {
		return runtimeservice.RuntimeArtifactRefInput{}, err
	}
	metadata := []byte(strings.TrimSpace(ref.GetMetadataJson()))
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}
	return runtimeservice.RuntimeArtifactRefInput{
		ArtifactType: artifactType,
		ExternalRef:  strings.TrimSpace(ref.GetExternalRef()),
		Digest:       strings.TrimSpace(ref.GetDigest()),
		MetadataJSON: metadata,
	}, nil
}

// PlacementConstraintsInputFromProto maps safe placement hints to the runtime domain.
func PlacementConstraintsInputFromProto(constraints *runtimev1.PlacementConstraints) (runtimeservice.PlacementConstraintsInput, error) {
	if constraints == nil {
		return runtimeservice.PlacementConstraintsInput{}, nil
	}
	projectID, err := optionalUUIDPtr(constraints.GetProjectId())
	if err != nil {
		return runtimeservice.PlacementConstraintsInput{}, err
	}
	repositoryIDs, err := repeatedUUIDs(constraints.GetRepositoryIds())
	if err != nil {
		return runtimeservice.PlacementConstraintsInput{}, err
	}
	preferredFleetScopeID, err := optionalUUIDPtr(constraints.GetPreferredFleetScopeId())
	if err != nil {
		return runtimeservice.PlacementConstraintsInput{}, err
	}
	return runtimeservice.PlacementConstraintsInput{
		ProjectID:             projectID,
		RepositoryIDs:         repositoryIDs,
		ServiceKeys:           append([]string(nil), constraints.GetServiceKeys()...),
		RuntimeProfile:        strings.TrimSpace(constraints.GetRuntimeProfile()),
		PreferredFleetScopeID: preferredFleetScopeID,
		RequiredCapabilities:  append([]string(nil), constraints.GetRequiredCapabilities()...),
		MetadataJSON:          []byte(strings.TrimSpace(constraints.GetMetadataJson())),
	}, nil
}

func optionalTime(text string) (*time.Time, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	value, err := requiredTime(trimmed)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func requiredTime(text string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
	if err != nil || value.IsZero() {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return value, nil
}

func workspaceMaterializationStatusesFromProto(statuses []runtimev1.WorkspaceMaterializationStatus) ([]enum.WorkspaceMaterializationStatus, error) {
	return repeatedEnumsFromProto(statuses, WorkspaceMaterializationStatusFromProto)
}

func slotStatusesFromProto(statuses []runtimev1.SlotStatus) ([]enum.SlotStatus, error) {
	return repeatedEnumsFromProto(statuses, SlotStatusFromProto)
}

func jobTypesFromProto(jobTypes []runtimev1.JobType) ([]enum.JobType, error) {
	return repeatedEnumsFromProto(jobTypes, JobTypeFromProto)
}

func jobStatusesFromProto(statuses []runtimev1.JobStatus) ([]enum.JobStatus, error) {
	return repeatedEnumsFromProto(statuses, JobStatusFromProto)
}

func runtimeArtifactTypesFromProto(artifactTypes []runtimev1.RuntimeArtifactType) ([]enum.RuntimeArtifactType, error) {
	return repeatedEnumsFromProto(artifactTypes, RuntimeArtifactTypeFromProto)
}

func repeatedEnumsFromProto[Proto any, Domain any](statuses []Proto, convert func(Proto) (Domain, error)) ([]Domain, error) {
	result := make([]Domain, 0, len(statuses))
	for _, status := range statuses {
		mapped, err := convert(status)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}
