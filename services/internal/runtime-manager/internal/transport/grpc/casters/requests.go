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
	fleetScopeID, err := preferredFleetScopeID(request.GetPlacementConstraints())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.PrepareRuntimeInput{}, err
	}
	return runtimeservice.PrepareRuntimeInput{
		AgentRunID:            agentRunID,
		RuntimeProfile:        strings.TrimSpace(request.GetRuntimeProfile()),
		RuntimeMode:           runtimeMode,
		WorkspacePolicy:       policy,
		PreferredFleetScopeID: fleetScopeID,
		Meta:                  meta,
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
	fleetScopeID, err := preferredFleetScopeID(request.GetPlacementConstraints())
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
		PreferredFleetScopeID: fleetScopeID,
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
	switch {
	case request == nil:
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
	leaseUntil, err := requiredTime(request.GetLeaseUntil())
	if err != nil {
		return runtimeservice.ExtendSlotLeaseInput{}, err
	}
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return runtimeservice.ExtendSlotLeaseInput{}, err
	}
	return runtimeservice.ExtendSlotLeaseInput{
		SlotID:     slotID,
		LeaseOwner: strings.TrimSpace(request.GetLeaseOwner()),
		LeaseUntil: leaseUntil,
		Meta:       meta,
	}, nil
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
	result := make([]value.WorkspaceSource, 0, len(sources))
	for _, source := range sources {
		mapped, err := workspaceSourceFromProto(source)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
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

func preferredFleetScopeID(constraints *runtimev1.PlacementConstraints) (*uuid.UUID, error) {
	if constraints == nil {
		return nil, nil
	}
	return optionalUUIDPtr(constraints.GetPreferredFleetScopeId())
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
