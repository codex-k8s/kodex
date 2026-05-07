package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimeservice "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

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
	slotID, err := requiredUUID(request.GetSlotId())
	if err != nil {
		return runtimeservice.GetSlotInput{}, err
	}
	meta, err := QueryMetaFromProto(request.GetMeta())
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

func preferredFleetScopeID(constraints *runtimev1.PlacementConstraints) (*uuid.UUID, error) {
	if constraints == nil {
		return nil, nil
	}
	return optionalUUIDPtr(constraints.GetPreferredFleetScopeId())
}

func requiredTime(text string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
	if err != nil || value.IsZero() {
		return time.Time{}, errs.ErrInvalidArgument
	}
	return value, nil
}

func slotStatusesFromProto(statuses []runtimev1.SlotStatus) ([]enum.SlotStatus, error) {
	result := make([]enum.SlotStatus, 0, len(statuses))
	for _, status := range statuses {
		mapped, err := SlotStatusFromProto(status)
		if err != nil {
			return nil, err
		}
		result = append(result, mapped)
	}
	return result, nil
}
