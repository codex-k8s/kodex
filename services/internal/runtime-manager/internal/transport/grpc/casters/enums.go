package casters

import (
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

var (
	runtimeModeFromProto = map[runtimev1.RuntimeMode]enum.RuntimeMode{
		runtimev1.RuntimeMode_RUNTIME_MODE_CODE_ONLY:            enum.RuntimeModeCodeOnly,
		runtimev1.RuntimeMode_RUNTIME_MODE_FULL_ENV:             enum.RuntimeModeFullEnv,
		runtimev1.RuntimeMode_RUNTIME_MODE_READ_ONLY_PRODUCTION: enum.RuntimeModeReadOnlyProduction,
	}
	runtimeModeToProto = invertEnumMap(runtimeModeFromProto)

	slotStatusFromProto = map[runtimev1.SlotStatus]enum.SlotStatus{
		runtimev1.SlotStatus_SLOT_STATUS_PREWARMED:       enum.SlotStatusPrewarmed,
		runtimev1.SlotStatus_SLOT_STATUS_RESERVED:        enum.SlotStatusReserved,
		runtimev1.SlotStatus_SLOT_STATUS_MATERIALIZING:   enum.SlotStatusMaterializing,
		runtimev1.SlotStatus_SLOT_STATUS_READY:           enum.SlotStatusReady,
		runtimev1.SlotStatus_SLOT_STATUS_IN_USE:          enum.SlotStatusInUse,
		runtimev1.SlotStatus_SLOT_STATUS_RELEASING:       enum.SlotStatusReleasing,
		runtimev1.SlotStatus_SLOT_STATUS_FAILED:          enum.SlotStatusFailed,
		runtimev1.SlotStatus_SLOT_STATUS_CLEANUP_PENDING: enum.SlotStatusCleanupPending,
		runtimev1.SlotStatus_SLOT_STATUS_CLEANED:         enum.SlotStatusCleaned,
	}
	slotStatusToProto = invertEnumMap(slotStatusFromProto)

	workspaceMaterializationStatusFromProto = map[runtimev1.WorkspaceMaterializationStatus]enum.WorkspaceMaterializationStatus{
		runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_PENDING:   enum.WorkspaceMaterializationStatusPending,
		runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_RUNNING:   enum.WorkspaceMaterializationStatusRunning,
		runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_COMPLETED: enum.WorkspaceMaterializationStatusCompleted,
		runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_FAILED:    enum.WorkspaceMaterializationStatusFailed,
		runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_CANCELLED: enum.WorkspaceMaterializationStatusCancelled,
	}
	workspaceMaterializationStatusToProto = invertEnumMap(workspaceMaterializationStatusFromProto)

	workspaceSourceKindFromProto = map[runtimev1.WorkspaceSourceKind]enum.WorkspaceSourceKind{
		runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_CODE:              enum.WorkspaceSourceKindCode,
		runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_DOCUMENTATION:     enum.WorkspaceSourceKindDocumentation,
		runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GUIDANCE_PACKAGE:  enum.WorkspaceSourceKindGuidancePackage,
		runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_GENERATED_CONTEXT: enum.WorkspaceSourceKindGeneratedContext,
	}
	workspaceSourceKindToProto = invertEnumMap(workspaceSourceKindFromProto)

	workspaceSourceAccessModeFromProto = map[runtimev1.WorkspaceSourceAccessMode]enum.WorkspaceSourceAccessMode{
		runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_READ:  enum.WorkspaceSourceAccessModeRead,
		runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_WRITE: enum.WorkspaceSourceAccessModeWrite,
	}
	workspaceSourceAccessModeToProto = invertEnumMap(workspaceSourceAccessModeFromProto)

	jobTypeFromProto = newJobTypeMap()
	jobTypeToProto   = invertEnumMap(jobTypeFromProto)

	jobStatusFromProto = newJobStatusMap()
	jobStatusToProto   = invertEnumMap(jobStatusFromProto)

	jobPriorityFromProto = map[runtimev1.JobPriority]enum.JobPriority{
		runtimev1.JobPriority_JOB_PRIORITY_LOW:      enum.JobPriorityLow,
		runtimev1.JobPriority_JOB_PRIORITY_NORMAL:   enum.JobPriorityNormal,
		runtimev1.JobPriority_JOB_PRIORITY_HIGH:     enum.JobPriorityHigh,
		runtimev1.JobPriority_JOB_PRIORITY_BLOCKING: enum.JobPriorityBlocking,
	}
	jobPriorityToProto = invertEnumMap(jobPriorityFromProto)

	jobStepStatusFromProto = map[runtimev1.JobStepStatus]enum.JobStepStatus{
		runtimev1.JobStepStatus_JOB_STEP_STATUS_PENDING:   enum.JobStepStatusPending,
		runtimev1.JobStepStatus_JOB_STEP_STATUS_RUNNING:   enum.JobStepStatusRunning,
		runtimev1.JobStepStatus_JOB_STEP_STATUS_SUCCEEDED: enum.JobStepStatusSucceeded,
		runtimev1.JobStepStatus_JOB_STEP_STATUS_FAILED:    enum.JobStepStatusFailed,
		runtimev1.JobStepStatus_JOB_STEP_STATUS_SKIPPED:   enum.JobStepStatusSkipped,
	}
	jobStepStatusToProto = invertEnumMap(jobStepStatusFromProto)

	runtimeArtifactTypeFromProto = map[runtimev1.RuntimeArtifactType]enum.RuntimeArtifactType{
		runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_IMAGE_REF:      enum.RuntimeArtifactTypeImageRef,
		runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_KUBERNETES_JOB: enum.RuntimeArtifactTypeKubernetesJob,
		runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_NAMESPACE:      enum.RuntimeArtifactTypeNamespace,
		runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_DEPLOYMENT:     enum.RuntimeArtifactTypeDeployment,
		runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_LOG_REF:        enum.RuntimeArtifactTypeLogRef,
		runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_MANIFEST_REF:   enum.RuntimeArtifactTypeManifestRef,
	}
	runtimeArtifactTypeToProto = invertEnumMap(runtimeArtifactTypeFromProto)
)

func enumFromProto[Proto comparable, Domain any](value Proto, values map[Proto]Domain) (Domain, error) {
	mapped, ok := values[value]
	if !ok {
		var zero Domain
		return zero, errs.ErrInvalidArgument
	}
	return mapped, nil
}

func enumToProto[Domain comparable, Proto any](value Domain, values map[Domain]Proto, zero Proto) Proto {
	if mapped, ok := values[value]; ok {
		return mapped
	}
	return zero
}

func invertEnumMap[Proto comparable, Domain comparable](values map[Proto]Domain) map[Domain]Proto {
	result := make(map[Domain]Proto, len(values))
	for protoValue, domainValue := range values {
		result[domainValue] = protoValue
	}
	return result
}

func newJobTypeMap() map[runtimev1.JobType]enum.JobType {
	return map[runtimev1.JobType]enum.JobType{
		runtimev1.JobType_JOB_TYPE_MIRROR:                    enum.JobTypeMirror,
		runtimev1.JobType_JOB_TYPE_BUILD:                     enum.JobTypeBuild,
		runtimev1.JobType_JOB_TYPE_DEPLOY:                    enum.JobTypeDeploy,
		runtimev1.JobType_JOB_TYPE_CLEANUP:                   enum.JobTypeCleanup,
		runtimev1.JobType_JOB_TYPE_HEALTH_CHECK:              enum.JobTypeHealthCheck,
		runtimev1.JobType_JOB_TYPE_HOUSEKEEPING:              enum.JobTypeHousekeeping,
		runtimev1.JobType_JOB_TYPE_WORKSPACE_MATERIALIZATION: enum.JobTypeWorkspaceMaterialization,
	}
}

func newJobStatusMap() map[runtimev1.JobStatus]enum.JobStatus {
	pairs := []struct {
		proto  runtimev1.JobStatus
		domain enum.JobStatus
	}{
		{proto: runtimev1.JobStatus_JOB_STATUS_PENDING, domain: enum.JobStatusPending},
		{proto: runtimev1.JobStatus_JOB_STATUS_CLAIMED, domain: enum.JobStatusClaimed},
		{proto: runtimev1.JobStatus_JOB_STATUS_RUNNING, domain: enum.JobStatusRunning},
		{proto: runtimev1.JobStatus_JOB_STATUS_SUCCEEDED, domain: enum.JobStatusSucceeded},
		{proto: runtimev1.JobStatus_JOB_STATUS_FAILED, domain: enum.JobStatusFailed},
		{proto: runtimev1.JobStatus_JOB_STATUS_CANCELLED, domain: enum.JobStatusCancelled},
		{proto: runtimev1.JobStatus_JOB_STATUS_TIMED_OUT, domain: enum.JobStatusTimedOut},
	}
	values := make(map[runtimev1.JobStatus]enum.JobStatus, len(pairs))
	for _, pair := range pairs {
		values[pair.proto] = pair.domain
	}
	return values
}

// RuntimeModeFromProto maps the runtime mode enum.
func RuntimeModeFromProto(mode runtimev1.RuntimeMode) (enum.RuntimeMode, error) {
	return enumFromProto(mode, runtimeModeFromProto)
}

// RuntimeModeToProto maps the runtime mode enum.
func RuntimeModeToProto(mode enum.RuntimeMode) runtimev1.RuntimeMode {
	return enumToProto(mode, runtimeModeToProto, runtimev1.RuntimeMode_RUNTIME_MODE_UNSPECIFIED)
}

// SlotStatusFromProto maps slot status filters.
func SlotStatusFromProto(status runtimev1.SlotStatus) (enum.SlotStatus, error) {
	return enumFromProto(status, slotStatusFromProto)
}

// SlotStatusToProto maps slot status.
func SlotStatusToProto(status enum.SlotStatus) runtimev1.SlotStatus {
	return enumToProto(status, slotStatusToProto, runtimev1.SlotStatus_SLOT_STATUS_UNSPECIFIED)
}

// WorkspaceMaterializationStatusFromProto maps materialization status.
func WorkspaceMaterializationStatusFromProto(status runtimev1.WorkspaceMaterializationStatus) (enum.WorkspaceMaterializationStatus, error) {
	return enumFromProto(status, workspaceMaterializationStatusFromProto)
}

// WorkspaceMaterializationStatusToProto maps materialization status.
func WorkspaceMaterializationStatusToProto(status enum.WorkspaceMaterializationStatus) runtimev1.WorkspaceMaterializationStatus {
	return enumToProto(status, workspaceMaterializationStatusToProto, runtimev1.WorkspaceMaterializationStatus_WORKSPACE_MATERIALIZATION_STATUS_UNSPECIFIED)
}

// WorkspaceSourceKindFromProto maps a workspace source kind.
func WorkspaceSourceKindFromProto(kind runtimev1.WorkspaceSourceKind) (enum.WorkspaceSourceKind, error) {
	return enumFromProto(kind, workspaceSourceKindFromProto)
}

// WorkspaceSourceKindToProto maps a workspace source kind.
func WorkspaceSourceKindToProto(kind enum.WorkspaceSourceKind) runtimev1.WorkspaceSourceKind {
	return enumToProto(kind, workspaceSourceKindToProto, runtimev1.WorkspaceSourceKind_WORKSPACE_SOURCE_KIND_UNSPECIFIED)
}

// WorkspaceSourceAccessModeFromProto maps source access mode.
func WorkspaceSourceAccessModeFromProto(mode runtimev1.WorkspaceSourceAccessMode) (enum.WorkspaceSourceAccessMode, error) {
	return enumFromProto(mode, workspaceSourceAccessModeFromProto)
}

// WorkspaceSourceAccessModeToProto maps source access mode.
func WorkspaceSourceAccessModeToProto(mode enum.WorkspaceSourceAccessMode) runtimev1.WorkspaceSourceAccessMode {
	return enumToProto(mode, workspaceSourceAccessModeToProto, runtimev1.WorkspaceSourceAccessMode_WORKSPACE_SOURCE_ACCESS_MODE_UNSPECIFIED)
}

// JobTypeFromProto maps a platform job type.
func JobTypeFromProto(jobType runtimev1.JobType) (enum.JobType, error) {
	return enumFromProto(jobType, jobTypeFromProto)
}

// JobTypeToProto maps a platform job type.
func JobTypeToProto(jobType enum.JobType) runtimev1.JobType {
	return enumToProto(jobType, jobTypeToProto, runtimev1.JobType_JOB_TYPE_UNSPECIFIED)
}

// JobStatusFromProto maps a job status filter.
func JobStatusFromProto(status runtimev1.JobStatus) (enum.JobStatus, error) {
	return enumFromProto(status, jobStatusFromProto)
}

// JobStatusToProto maps a job status.
func JobStatusToProto(status enum.JobStatus) runtimev1.JobStatus {
	return enumToProto(status, jobStatusToProto, runtimev1.JobStatus_JOB_STATUS_UNSPECIFIED)
}

// JobPriorityFromProto maps a job priority.
func JobPriorityFromProto(priority runtimev1.JobPriority) (enum.JobPriority, error) {
	return enumFromProto(priority, jobPriorityFromProto)
}

// JobPriorityToProto maps a job priority.
func JobPriorityToProto(priority enum.JobPriority) runtimev1.JobPriority {
	return enumToProto(priority, jobPriorityToProto, runtimev1.JobPriority_JOB_PRIORITY_UNSPECIFIED)
}

// JobStepStatusFromProto maps a job step status.
func JobStepStatusFromProto(status runtimev1.JobStepStatus) (enum.JobStepStatus, error) {
	return enumFromProto(status, jobStepStatusFromProto)
}

// JobStepStatusToProto maps a job step status.
func JobStepStatusToProto(status enum.JobStepStatus) runtimev1.JobStepStatus {
	return enumToProto(status, jobStepStatusToProto, runtimev1.JobStepStatus_JOB_STEP_STATUS_UNSPECIFIED)
}

// RuntimeArtifactTypeFromProto maps an external runtime artifact type.
func RuntimeArtifactTypeFromProto(artifactType runtimev1.RuntimeArtifactType) (enum.RuntimeArtifactType, error) {
	return enumFromProto(artifactType, runtimeArtifactTypeFromProto)
}

// RuntimeArtifactTypeToProto maps an external runtime artifact type.
func RuntimeArtifactTypeToProto(artifactType enum.RuntimeArtifactType) runtimev1.RuntimeArtifactType {
	return enumToProto(artifactType, runtimeArtifactTypeToProto, runtimev1.RuntimeArtifactType_RUNTIME_ARTIFACT_TYPE_UNSPECIFIED)
}
