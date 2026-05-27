// Package enum contains agent-manager domain enumerations.
package enum

type AgentScopeType string

const (
	AgentScopeTypePlatform     AgentScopeType = "platform"
	AgentScopeTypeOrganization AgentScopeType = "organization"
	AgentScopeTypeProject      AgentScopeType = "project"
	AgentScopeTypeRepository   AgentScopeType = "repository"
)

type FlowStatus string

const (
	FlowStatusDraft    FlowStatus = "draft"
	FlowStatusActive   FlowStatus = "active"
	FlowStatusDisabled FlowStatus = "disabled"
	FlowStatusArchived FlowStatus = "archived"
)

type FlowVersionStatus string

const (
	FlowVersionStatusDraft      FlowVersionStatus = "draft"
	FlowVersionStatusActive     FlowVersionStatus = "active"
	FlowVersionStatusSuperseded FlowVersionStatus = "superseded"
	FlowVersionStatusRejected   FlowVersionStatus = "rejected"
)

type StageType string

const (
	StageTypeWork    StageType = "work"
	StageTypeReview  StageType = "review"
	StageTypeGate    StageType = "gate"
	StageTypeRelease StageType = "release"
	StageTypeOps     StageType = "ops"
	StageTypeCustom  StageType = "custom"
)

type StageRoleBindingKind string

const (
	StageRoleBindingKindExecutor   StageRoleBindingKind = "executor"
	StageRoleBindingKindReviewer   StageRoleBindingKind = "reviewer"
	StageRoleBindingKindGatekeeper StageRoleBindingKind = "gatekeeper"
	StageRoleBindingKindQA         StageRoleBindingKind = "qa"
	StageRoleBindingKindObserver   StageRoleBindingKind = "observer"
	StageRoleBindingKindCustom     StageRoleBindingKind = "custom"
)

type RoleKind string

const (
	RoleKindWorker     RoleKind = "worker"
	RoleKindReviewer   RoleKind = "reviewer"
	RoleKindGatekeeper RoleKind = "gatekeeper"
	RoleKindManager    RoleKind = "manager"
	RoleKindQA         RoleKind = "qa"
	RoleKindOps        RoleKind = "ops"
	RoleKindCustom     RoleKind = "custom"
)

type RoleStatus string

const (
	RoleStatusDraft    RoleStatus = "draft"
	RoleStatusActive   RoleStatus = "active"
	RoleStatusDisabled RoleStatus = "disabled"
	RoleStatusArchived RoleStatus = "archived"
)

type PromptKind string

const (
	PromptKindWork    PromptKind = "work"
	PromptKindRevise  PromptKind = "revise"
	PromptKindReview  PromptKind = "review"
	PromptKindManager PromptKind = "manager"
	PromptKindCustom  PromptKind = "custom"
)

type PromptVersionStatus string

const (
	PromptVersionStatusDraft      PromptVersionStatus = "draft"
	PromptVersionStatusActive     PromptVersionStatus = "active"
	PromptVersionStatusSuperseded PromptVersionStatus = "superseded"
	PromptVersionStatusRejected   PromptVersionStatus = "rejected"
)

type AgentSessionStatus string

const (
	AgentSessionStatusOpen      AgentSessionStatus = "open"
	AgentSessionStatusWaiting   AgentSessionStatus = "waiting"
	AgentSessionStatusCompleted AgentSessionStatus = "completed"
	AgentSessionStatusFailed    AgentSessionStatus = "failed"
	AgentSessionStatusCancelled AgentSessionStatus = "cancelled"
)

type AgentRunStatus string

const (
	AgentRunStatusRequested AgentRunStatus = "requested"
	AgentRunStatusStarting  AgentRunStatus = "starting"
	AgentRunStatusRunning   AgentRunStatus = "running"
	AgentRunStatusWaiting   AgentRunStatus = "waiting"
	AgentRunStatusCompleted AgentRunStatus = "completed"
	AgentRunStatusFailed    AgentRunStatus = "failed"
	AgentRunStatusCancelled AgentRunStatus = "cancelled"
)

type AgentSessionSnapshotKind string

const (
	AgentSessionSnapshotKindTurnCheckpoint     AgentSessionSnapshotKind = "turn_checkpoint"
	AgentSessionSnapshotKindRunCompletion      AgentSessionSnapshotKind = "run_completion"
	AgentSessionSnapshotKindManualCheckpoint   AgentSessionSnapshotKind = "manual_checkpoint"
	AgentSessionSnapshotKindRecoveryCheckpoint AgentSessionSnapshotKind = "recovery_checkpoint"
)

type AcceptanceCheckKind string

const (
	AcceptanceCheckKindArtifact   AcceptanceCheckKind = "artifact"
	AcceptanceCheckKindWatermark  AcceptanceCheckKind = "watermark"
	AcceptanceCheckKindPolicy     AcceptanceCheckKind = "policy"
	AcceptanceCheckKindRoleResult AcceptanceCheckKind = "role_result"
	AcceptanceCheckKindHumanGate  AcceptanceCheckKind = "human_gate"
	AcceptanceCheckKindFollowUp   AcceptanceCheckKind = "follow_up"
)

type AcceptanceStatus string

const (
	AcceptanceStatusPending AcceptanceStatus = "pending"
	AcceptanceStatusPassed  AcceptanceStatus = "passed"
	AcceptanceStatusFailed  AcceptanceStatus = "failed"
	AcceptanceStatusWaiting AcceptanceStatus = "waiting"
	AcceptanceStatusSkipped AcceptanceStatus = "skipped"
)

type FollowUpIntentStatus string

const (
	FollowUpIntentStatusPlanned   FollowUpIntentStatus = "planned"
	FollowUpIntentStatusRequested FollowUpIntentStatus = "requested"
	FollowUpIntentStatusCreated   FollowUpIntentStatus = "created"
	FollowUpIntentStatusFailed    FollowUpIntentStatus = "failed"
	FollowUpIntentStatusCancelled FollowUpIntentStatus = "cancelled"
	FollowUpIntentStatusUpdated   FollowUpIntentStatus = "updated"
	FollowUpIntentStatusCommented FollowUpIntentStatus = "commented"
)

type AgentActivityKind string

const (
	AgentActivityKindLifecycle      AgentActivityKind = "lifecycle"
	AgentActivityKindToolUse        AgentActivityKind = "tool_use"
	AgentActivityKindToolResult     AgentActivityKind = "tool_result"
	AgentActivityKindPermission     AgentActivityKind = "permission"
	AgentActivityKindProviderSignal AgentActivityKind = "provider_signal"
	AgentActivityKindRuntimeSignal  AgentActivityKind = "runtime_signal"
	AgentActivityKindCheckpoint     AgentActivityKind = "checkpoint"
	AgentActivityKindOther          AgentActivityKind = "other"
)

type AgentActivityStatus string

const (
	AgentActivityStatusPlanned   AgentActivityStatus = "planned"
	AgentActivityStatusStarted   AgentActivityStatus = "started"
	AgentActivityStatusSucceeded AgentActivityStatus = "succeeded"
	AgentActivityStatusFailed    AgentActivityStatus = "failed"
	AgentActivityStatusDenied    AgentActivityStatus = "denied"
	AgentActivityStatusWaiting   AgentActivityStatus = "waiting"
	AgentActivityStatusCancelled AgentActivityStatus = "cancelled"
	AgentActivityStatusSkipped   AgentActivityStatus = "skipped"
)

type CommandAggregateType string

const (
	CommandAggregateTypeFlow                  CommandAggregateType = "flow"
	CommandAggregateTypeFlowVersion           CommandAggregateType = "flow_version"
	CommandAggregateTypeRoleProfile           CommandAggregateType = "role_profile"
	CommandAggregateTypePromptTemplate        CommandAggregateType = "prompt_template"
	CommandAggregateTypePromptTemplateVersion CommandAggregateType = "prompt_template_version"
	CommandAggregateTypeSession               CommandAggregateType = "session"
	CommandAggregateTypeRun                   CommandAggregateType = "run"
	CommandAggregateTypeSessionStateSnapshot  CommandAggregateType = "session_state_snapshot"
	CommandAggregateTypeAcceptance            CommandAggregateType = "acceptance"
	CommandAggregateTypeFollowUp              CommandAggregateType = "follow_up"
	CommandAggregateTypeActivity              CommandAggregateType = "activity"
)
