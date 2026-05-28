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

	agentRunRunnerModeFromProto = map[runtimev1.AgentRunRunnerMode]enum.AgentRunRunnerMode{
		runtimev1.AgentRunRunnerMode_AGENT_RUN_RUNNER_MODE_CODEX_AGENT: enum.AgentRunRunnerModeCodexAgent,
	}
	agentRunRunnerModeToProto = invertEnumMap(agentRunRunnerModeFromProto)

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

	cleanupPolicyStatusFromProto = map[runtimev1.CleanupPolicyStatus]enum.CleanupPolicyStatus{
		runtimev1.CleanupPolicyStatus_CLEANUP_POLICY_STATUS_ACTIVE:     enum.CleanupPolicyStatusActive,
		runtimev1.CleanupPolicyStatus_CLEANUP_POLICY_STATUS_DISABLED:   enum.CleanupPolicyStatusDisabled,
		runtimev1.CleanupPolicyStatus_CLEANUP_POLICY_STATUS_SUPERSEDED: enum.CleanupPolicyStatusSuperseded,
	}
	cleanupPolicyStatusToProto = invertEnumMap(cleanupPolicyStatusFromProto)

	runtimeScopeTypeFromProto = map[runtimev1.RuntimeScopeType]enum.RuntimeScopeType{
		runtimev1.RuntimeScopeType_RUNTIME_SCOPE_TYPE_PLATFORM:        enum.RuntimeScopePlatform,
		runtimev1.RuntimeScopeType_RUNTIME_SCOPE_TYPE_ORGANIZATION:    enum.RuntimeScopeOrganization,
		runtimev1.RuntimeScopeType_RUNTIME_SCOPE_TYPE_PROJECT:         enum.RuntimeScopeProject,
		runtimev1.RuntimeScopeType_RUNTIME_SCOPE_TYPE_REPOSITORY:      enum.RuntimeScopeRepository,
		runtimev1.RuntimeScopeType_RUNTIME_SCOPE_TYPE_RUNTIME_PROFILE: enum.RuntimeScopeRuntimeProfile,
	}
	runtimeScopeTypeToProto = invertEnumMap(runtimeScopeTypeFromProto)

	prewarmPoolScopeTypeFromProto = map[runtimev1.PrewarmPoolScopeType]enum.PrewarmPoolScopeType{
		runtimev1.PrewarmPoolScopeType_PREWARM_POOL_SCOPE_TYPE_PLATFORM:     enum.PrewarmPoolScopePlatform,
		runtimev1.PrewarmPoolScopeType_PREWARM_POOL_SCOPE_TYPE_ORGANIZATION: enum.PrewarmPoolScopeOrganization,
		runtimev1.PrewarmPoolScopeType_PREWARM_POOL_SCOPE_TYPE_PROJECT:      enum.PrewarmPoolScopeProject,
		runtimev1.PrewarmPoolScopeType_PREWARM_POOL_SCOPE_TYPE_REPOSITORY:   enum.PrewarmPoolScopeRepository,
	}
	prewarmPoolScopeTypeToProto = invertEnumMap(prewarmPoolScopeTypeFromProto)

	prewarmPoolStatusFromProto = map[runtimev1.PrewarmPoolStatus]enum.PrewarmPoolStatus{
		runtimev1.PrewarmPoolStatus_PREWARM_POOL_STATUS_ACTIVE:   enum.PrewarmPoolStatusActive,
		runtimev1.PrewarmPoolStatus_PREWARM_POOL_STATUS_PAUSED:   enum.PrewarmPoolStatusPaused,
		runtimev1.PrewarmPoolStatus_PREWARM_POOL_STATUS_DISABLED: enum.PrewarmPoolStatusDisabled,
	}
	prewarmPoolStatusToProto = invertEnumMap(prewarmPoolStatusFromProto)

	capacityStatusToProto = map[enum.CapacityStatus]runtimev1.CapacityStatus{
		enum.CapacityStatusOK:           runtimev1.CapacityStatus_CAPACITY_STATUS_OK,
		enum.CapacityStatusDegraded:     runtimev1.CapacityStatus_CAPACITY_STATUS_DEGRADED,
		enum.CapacityStatusInsufficient: runtimev1.CapacityStatus_CAPACITY_STATUS_INSUFFICIENT,
	}
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
		runtimev1.JobType_JOB_TYPE_AGENT_RUN:                 enum.JobTypeAgentRun,
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

// AgentRunRunnerModeFromProto maps an agent_run runner mode from proto.
func AgentRunRunnerModeFromProto(mode runtimev1.AgentRunRunnerMode) (enum.AgentRunRunnerMode, error) {
	return enumFromProto(mode, agentRunRunnerModeFromProto)
}

// AgentRunRunnerModeToProto maps an agent_run runner mode to proto.
func AgentRunRunnerModeToProto(mode enum.AgentRunRunnerMode) runtimev1.AgentRunRunnerMode {
	return enumToProto(mode, agentRunRunnerModeToProto, runtimev1.AgentRunRunnerMode_AGENT_RUN_RUNNER_MODE_UNSPECIFIED)
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

// CleanupPolicyStatusFromProto maps cleanup policy status.
func CleanupPolicyStatusFromProto(status runtimev1.CleanupPolicyStatus) (enum.CleanupPolicyStatus, error) {
	return enumFromProto(status, cleanupPolicyStatusFromProto)
}

// CleanupPolicyStatusToProto maps cleanup policy status.
func CleanupPolicyStatusToProto(status enum.CleanupPolicyStatus) runtimev1.CleanupPolicyStatus {
	return enumToProto(status, cleanupPolicyStatusToProto, runtimev1.CleanupPolicyStatus_CLEANUP_POLICY_STATUS_UNSPECIFIED)
}

// RuntimeScopeTypeFromProto maps cleanup scope type.
func RuntimeScopeTypeFromProto(scope runtimev1.RuntimeScopeType) (enum.RuntimeScopeType, error) {
	return enumFromProto(scope, runtimeScopeTypeFromProto)
}

// RuntimeScopeTypeToProto maps cleanup scope type.
func RuntimeScopeTypeToProto(scope enum.RuntimeScopeType) runtimev1.RuntimeScopeType {
	return enumToProto(scope, runtimeScopeTypeToProto, runtimev1.RuntimeScopeType_RUNTIME_SCOPE_TYPE_UNSPECIFIED)
}

// PrewarmPoolScopeTypeFromProto maps prewarm pool scope type.
func PrewarmPoolScopeTypeFromProto(scope runtimev1.PrewarmPoolScopeType) (enum.PrewarmPoolScopeType, error) {
	return enumFromProto(scope, prewarmPoolScopeTypeFromProto)
}

// PrewarmPoolScopeTypeToProto maps prewarm pool scope type.
func PrewarmPoolScopeTypeToProto(scope enum.PrewarmPoolScopeType) runtimev1.PrewarmPoolScopeType {
	return enumToProto(scope, prewarmPoolScopeTypeToProto, runtimev1.PrewarmPoolScopeType_PREWARM_POOL_SCOPE_TYPE_UNSPECIFIED)
}

// PrewarmPoolStatusFromProto maps prewarm pool status.
func PrewarmPoolStatusFromProto(status runtimev1.PrewarmPoolStatus) (enum.PrewarmPoolStatus, error) {
	return enumFromProto(status, prewarmPoolStatusFromProto)
}

// PrewarmPoolStatusToProto maps prewarm pool status.
func PrewarmPoolStatusToProto(status enum.PrewarmPoolStatus) runtimev1.PrewarmPoolStatus {
	return enumToProto(status, prewarmPoolStatusToProto, runtimev1.PrewarmPoolStatus_PREWARM_POOL_STATUS_UNSPECIFIED)
}

// CapacityStatusToProto maps prewarm capacity status.
func CapacityStatusToProto(status enum.CapacityStatus) runtimev1.CapacityStatus {
	return enumToProto(status, capacityStatusToProto, runtimev1.CapacityStatus_CAPACITY_STATUS_UNSPECIFIED)
}
