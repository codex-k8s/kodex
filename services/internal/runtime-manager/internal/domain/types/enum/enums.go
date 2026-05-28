// Package enum contains closed runtime-manager vocabularies.
package enum

// RuntimeMode classifies runtime isolation level.
type RuntimeMode string

const (
	RuntimeModeCodeOnly           RuntimeMode = "code_only"
	RuntimeModeFullEnv            RuntimeMode = "full_env"
	RuntimeModeReadOnlyProduction RuntimeMode = "read_only_production"
	RuntimeModePlatformJob        RuntimeMode = "platform_job"
)

// SlotStatus is the slot lifecycle status.
type SlotStatus string

const (
	SlotStatusPrewarmed      SlotStatus = "prewarmed"
	SlotStatusReserved       SlotStatus = "reserved"
	SlotStatusMaterializing  SlotStatus = "materializing"
	SlotStatusReady          SlotStatus = "ready"
	SlotStatusInUse          SlotStatus = "in_use"
	SlotStatusReleasing      SlotStatus = "releasing"
	SlotStatusFailed         SlotStatus = "failed"
	SlotStatusCleanupPending SlotStatus = "cleanup_pending"
	SlotStatusCleaned        SlotStatus = "cleaned"
)

// WorkspaceMaterializationStatus is the workspace preparation status.
type WorkspaceMaterializationStatus string

const (
	WorkspaceMaterializationStatusPending   WorkspaceMaterializationStatus = "pending"
	WorkspaceMaterializationStatusRunning   WorkspaceMaterializationStatus = "running"
	WorkspaceMaterializationStatusCompleted WorkspaceMaterializationStatus = "completed"
	WorkspaceMaterializationStatusFailed    WorkspaceMaterializationStatus = "failed"
	WorkspaceMaterializationStatusCancelled WorkspaceMaterializationStatus = "cancelled"
)

// WorkspaceSourceKind classifies one materialized source.
type WorkspaceSourceKind string

const (
	WorkspaceSourceKindCode             WorkspaceSourceKind = "code"
	WorkspaceSourceKindDocumentation    WorkspaceSourceKind = "documentation"
	WorkspaceSourceKindGuidancePackage  WorkspaceSourceKind = "guidance_package"
	WorkspaceSourceKindGeneratedContext WorkspaceSourceKind = "generated_context"
)

// WorkspaceSourceAccessMode controls whether a materialized source is editable.
type WorkspaceSourceAccessMode string

const (
	WorkspaceSourceAccessModeRead  WorkspaceSourceAccessMode = "read"
	WorkspaceSourceAccessModeWrite WorkspaceSourceAccessMode = "write"
)

// JobType classifies platform technical jobs.
type JobType string

const (
	JobTypeMirror                   JobType = "mirror"
	JobTypeBuild                    JobType = "build"
	JobTypeDeploy                   JobType = "deploy"
	JobTypeCleanup                  JobType = "cleanup"
	JobTypeHealthCheck              JobType = "health_check"
	JobTypeHousekeeping             JobType = "housekeeping"
	JobTypeWorkspaceMaterialization JobType = "workspace_materialization"
	JobTypeAgentRun                 JobType = "agent_run"
)

// JobStatus is the platform job lifecycle status.
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusClaimed   JobStatus = "claimed"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
	JobStatusTimedOut  JobStatus = "timed_out"
)

// JobPriority classifies job scheduling priority.
type JobPriority string

const (
	JobPriorityLow      JobPriority = "low"
	JobPriorityNormal   JobPriority = "normal"
	JobPriorityHigh     JobPriority = "high"
	JobPriorityBlocking JobPriority = "blocking"
)

// AgentRunRunnerMode выбирает фиксированный режим agent runner.
type AgentRunRunnerMode string

const (
	AgentRunRunnerModeCodexAgent AgentRunRunnerMode = "codex_agent"
)

// JobStepStatus is one job step status.
type JobStepStatus string

const (
	JobStepStatusPending   JobStepStatus = "pending"
	JobStepStatusRunning   JobStepStatus = "running"
	JobStepStatusSucceeded JobStepStatus = "succeeded"
	JobStepStatusFailed    JobStepStatus = "failed"
	JobStepStatusSkipped   JobStepStatus = "skipped"
)

// RuntimeArtifactType classifies references to external runtime artifacts.
type RuntimeArtifactType string

const (
	RuntimeArtifactTypeImageRef      RuntimeArtifactType = "image_ref"
	RuntimeArtifactTypeKubernetesJob RuntimeArtifactType = "kubernetes_job"
	RuntimeArtifactTypeNamespace     RuntimeArtifactType = "namespace"
	RuntimeArtifactTypeDeployment    RuntimeArtifactType = "deployment"
	RuntimeArtifactTypeLogRef        RuntimeArtifactType = "log_ref"
	RuntimeArtifactTypeManifestRef   RuntimeArtifactType = "manifest_ref"
)

// RuntimeScopeType classifies cleanup policy scopes.
type RuntimeScopeType string

const (
	RuntimeScopePlatform       RuntimeScopeType = "platform"
	RuntimeScopeOrganization   RuntimeScopeType = "organization"
	RuntimeScopeProject        RuntimeScopeType = "project"
	RuntimeScopeRepository     RuntimeScopeType = "repository"
	RuntimeScopeRuntimeProfile RuntimeScopeType = "runtime_profile"
)

// PrewarmPoolScopeType classifies prewarm pool scopes.
type PrewarmPoolScopeType string

const (
	PrewarmPoolScopePlatform     PrewarmPoolScopeType = "platform"
	PrewarmPoolScopeOrganization PrewarmPoolScopeType = "organization"
	PrewarmPoolScopeProject      PrewarmPoolScopeType = "project"
	PrewarmPoolScopeRepository   PrewarmPoolScopeType = "repository"
)

// CleanupPolicyStatus is the cleanup policy lifecycle status.
type CleanupPolicyStatus string

const (
	CleanupPolicyStatusActive     CleanupPolicyStatus = "active"
	CleanupPolicyStatusDisabled   CleanupPolicyStatus = "disabled"
	CleanupPolicyStatusSuperseded CleanupPolicyStatus = "superseded"
)

// PrewarmPoolStatus is the prewarm pool lifecycle status.
type PrewarmPoolStatus string

const (
	PrewarmPoolStatusActive   PrewarmPoolStatus = "active"
	PrewarmPoolStatusPaused   PrewarmPoolStatus = "paused"
	PrewarmPoolStatusDisabled PrewarmPoolStatus = "disabled"
)

// CapacityStatus describes whether actual prewarm capacity matches policy.
type CapacityStatus string

const (
	CapacityStatusOK           CapacityStatus = "ok"
	CapacityStatusDegraded     CapacityStatus = "degraded"
	CapacityStatusInsufficient CapacityStatus = "insufficient"
)
