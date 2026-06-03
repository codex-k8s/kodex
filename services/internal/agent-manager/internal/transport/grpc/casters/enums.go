package casters

import (
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
)

var scopeTypesFromProto = map[agentsv1.AgentScopeType]enum.AgentScopeType{
	agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PLATFORM:     enum.AgentScopeTypePlatform,
	agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_ORGANIZATION: enum.AgentScopeTypeOrganization,
	agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT:      enum.AgentScopeTypeProject,
	agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_REPOSITORY:   enum.AgentScopeTypeRepository,
}

var flowStatusesFromProto = map[agentsv1.FlowStatus]enum.FlowStatus{
	agentsv1.FlowStatus_FLOW_STATUS_DRAFT:    enum.FlowStatusDraft,
	agentsv1.FlowStatus_FLOW_STATUS_ACTIVE:   enum.FlowStatusActive,
	agentsv1.FlowStatus_FLOW_STATUS_DISABLED: enum.FlowStatusDisabled,
	agentsv1.FlowStatus_FLOW_STATUS_ARCHIVED: enum.FlowStatusArchived,
}

var flowVersionStatusesToProto = map[enum.FlowVersionStatus]agentsv1.FlowVersionStatus{
	enum.FlowVersionStatusDraft:      agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_DRAFT,
	enum.FlowVersionStatusActive:     agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_ACTIVE,
	enum.FlowVersionStatusSuperseded: agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_SUPERSEDED,
	enum.FlowVersionStatusRejected:   agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_REJECTED,
}

var stageTypesFromProto = enumMap(
	enumPair(agentsv1.StageType_STAGE_TYPE_WORK, enum.StageTypeWork),
	enumPair(agentsv1.StageType_STAGE_TYPE_REVIEW, enum.StageTypeReview),
	enumPair(agentsv1.StageType_STAGE_TYPE_GATE, enum.StageTypeGate),
	enumPair(agentsv1.StageType_STAGE_TYPE_RELEASE, enum.StageTypeRelease),
	enumPair(agentsv1.StageType_STAGE_TYPE_OPS, enum.StageTypeOps),
	enumPair(agentsv1.StageType_STAGE_TYPE_CUSTOM, enum.StageTypeCustom),
)

var stageRoleBindingKindsFromProto = stageRoleBindingKindValues()

var roleKindsFromProto = roleKindValues()

var roleStatusesFromProto = map[agentsv1.RoleStatus]enum.RoleStatus{
	agentsv1.RoleStatus_ROLE_STATUS_DRAFT:    enum.RoleStatusDraft,
	agentsv1.RoleStatus_ROLE_STATUS_ACTIVE:   enum.RoleStatusActive,
	agentsv1.RoleStatus_ROLE_STATUS_DISABLED: enum.RoleStatusDisabled,
	agentsv1.RoleStatus_ROLE_STATUS_ARCHIVED: enum.RoleStatusArchived,
}

var promptKindsFromProto = map[agentsv1.PromptKind]enum.PromptKind{
	agentsv1.PromptKind_PROMPT_KIND_WORK:    enum.PromptKindWork,
	agentsv1.PromptKind_PROMPT_KIND_REVISE:  enum.PromptKindRevise,
	agentsv1.PromptKind_PROMPT_KIND_REVIEW:  enum.PromptKindReview,
	agentsv1.PromptKind_PROMPT_KIND_MANAGER: enum.PromptKindManager,
	agentsv1.PromptKind_PROMPT_KIND_CUSTOM:  enum.PromptKindCustom,
}

var promptVersionStatusesFromProto = map[agentsv1.PromptVersionStatus]enum.PromptVersionStatus{
	agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_DRAFT:      enum.PromptVersionStatusDraft,
	agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_ACTIVE:     enum.PromptVersionStatusActive,
	agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_SUPERSEDED: enum.PromptVersionStatusSuperseded,
	agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_REJECTED:   enum.PromptVersionStatusRejected,
}

var agentRunStatusesFromProto = agentRunStatusValues()

var agentRunnerRunStatesFromProto = agentRunnerRunStateValues()

var agentSessionSnapshotKindsFromProto = map[agentsv1.AgentSessionSnapshotKind]enum.AgentSessionSnapshotKind{
	agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_TURN_CHECKPOINT:     enum.AgentSessionSnapshotKindTurnCheckpoint,
	agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_RUN_COMPLETION:      enum.AgentSessionSnapshotKindRunCompletion,
	agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_MANUAL_CHECKPOINT:   enum.AgentSessionSnapshotKindManualCheckpoint,
	agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_RECOVERY_CHECKPOINT: enum.AgentSessionSnapshotKindRecoveryCheckpoint,
}

var acceptanceCheckKindsFromProto = acceptanceCheckKindValues()

var acceptanceStatusesFromProto = enumMap(
	enumPair(agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_PENDING, enum.AcceptanceStatusPending),
	enumPair(agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_PASSED, enum.AcceptanceStatusPassed),
	enumPair(agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_FAILED, enum.AcceptanceStatusFailed),
	enumPair(agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_WAITING, enum.AcceptanceStatusWaiting),
	enumPair(agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_SKIPPED, enum.AcceptanceStatusSkipped),
)

var followUpIntentStatusesToProto = enumMap(
	enumPair(enum.FollowUpIntentStatusPlanned, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_PLANNED),
	enumPair(enum.FollowUpIntentStatusRequested, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_REQUESTED),
	enumPair(enum.FollowUpIntentStatusCreated, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_CREATED),
	enumPair(enum.FollowUpIntentStatusFailed, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_FAILED),
	enumPair(enum.FollowUpIntentStatusCancelled, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_CANCELLED),
	enumPair(enum.FollowUpIntentStatusUpdated, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_UPDATED),
	enumPair(enum.FollowUpIntentStatusCommented, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_COMMENTED),
	enumPair(enum.FollowUpIntentStatusReviewSignaled, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_REVIEW_SIGNALED),
)

var agentActivityKindsFromProto = agentActivityKindValues()

var agentActivityStatusesFromProto = agentActivityStatusValues()

var humanGateStatusesFromProto = enumMap(
	enumPair(agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_REQUESTED, enum.HumanGateStatusRequested),
	enumPair(agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_WAITING, enum.HumanGateStatusWaiting),
	enumPair(agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_RESOLVED, enum.HumanGateStatusResolved),
	enumPair(agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_FAILED, enum.HumanGateStatusFailed),
	enumPair(agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_CANCELLED, enum.HumanGateStatusCancelled),
)

var humanGateOutcomesFromProto = enumMap(
	enumPair(agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_NONE, enum.HumanGateOutcomeNone),
	enumPair(agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_APPROVE, enum.HumanGateOutcomeApprove),
	enumPair(agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_REJECT, enum.HumanGateOutcomeReject),
	enumPair(agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_REQUEST_CHANGES, enum.HumanGateOutcomeRequestChanges),
	enumPair(agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_ANSWER, enum.HumanGateOutcomeAnswer),
)

var selfDeployPlanStatusesFromProto = enumMap(
	enumPair(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL, enum.SelfDeployPlanStatusPendingApproval),
	enumPair(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED, enum.SelfDeployPlanStatusApproved),
	enumPair(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_REJECTED, enum.SelfDeployPlanStatusRejected),
	enumPair(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_CANCELLED, enum.SelfDeployPlanStatusCancelled),
	enumPair(agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_FAILED, enum.SelfDeployPlanStatusFailed),
)

var selfDeployPathCategoriesFromProto = enumMap(
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICES_POLICY, enum.SelfDeployPathCategoryServicesPolicy),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICE_SOURCE, enum.SelfDeployPathCategoryServiceSource),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICE_CONFIG, enum.SelfDeployPathCategoryServiceConfig),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_DEPLOY_MANIFEST, enum.SelfDeployPathCategoryDeployManifest),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_RUNTIME_CONFIG, enum.SelfDeployPathCategoryRuntimeConfig),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_DOCUMENTATION, enum.SelfDeployPathCategoryDocumentation),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_TEST, enum.SelfDeployPathCategoryTest),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_PLATFORM_POLICY, enum.SelfDeployPathCategoryPlatformPolicy),
	enumPair(agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_OTHER, enum.SelfDeployPathCategoryOther),
)

var selfDeployRuntimeJobTypesFromProto = map[runtimev1.JobType]enum.SelfDeployRuntimeJobType{
	runtimev1.JobType_JOB_TYPE_BUILD:        enum.SelfDeployRuntimeJobTypeBuild,
	runtimev1.JobType_JOB_TYPE_DEPLOY:       enum.SelfDeployRuntimeJobTypeDeploy,
	runtimev1.JobType_JOB_TYPE_HEALTH_CHECK: enum.SelfDeployRuntimeJobTypeHealthCheck,
}

var runtimeObservationStatesToProto = enumMap(
	enumPair(agentservice.RuntimeObservationStateNotCreated, agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_NOT_CREATED),
	enumPair(agentservice.RuntimeObservationStateStoredRef, agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_STORED_REF),
	enumPair(agentservice.RuntimeObservationStateLive, agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_LIVE),
	enumPair(agentservice.RuntimeObservationStateUnavailable, agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_UNAVAILABLE),
	enumPair(agentservice.RuntimeObservationStateConflict, agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_CONFLICT),
)

var runtimeJobStatusesToProto = enumMap(
	enumPair(agentservice.RuntimeJobStatusPending, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_PENDING),
	enumPair(agentservice.RuntimeJobStatusClaimed, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_CLAIMED),
	enumPair(agentservice.RuntimeJobStatusRunning, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_RUNNING),
	enumPair(agentservice.RuntimeJobStatusSucceeded, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_SUCCEEDED),
	enumPair(agentservice.RuntimeJobStatusFailed, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_FAILED),
	enumPair(agentservice.RuntimeJobStatusCancelled, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_CANCELLED),
	enumPair(agentservice.RuntimeJobStatusTimedOut, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_TIMED_OUT),
)

var agentSessionStatusesToProto = enumMap(
	enumPair(enum.AgentSessionStatusOpen, agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_OPEN),
	enumPair(enum.AgentSessionStatusWaiting, agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_WAITING),
	enumPair(enum.AgentSessionStatusCompleted, agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_COMPLETED),
	enumPair(enum.AgentSessionStatusFailed, agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_FAILED),
	enumPair(enum.AgentSessionStatusCancelled, agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_CANCELLED),
)

var (
	scopeTypesToProto                = reverseEnumMap(scopeTypesFromProto)
	flowStatusesToProto              = reverseEnumMap(flowStatusesFromProto)
	stageTypesToProto                = reverseEnumMap(stageTypesFromProto)
	stageRoleBindingKindsToProto     = reverseEnumMap(stageRoleBindingKindsFromProto)
	roleKindsToProto                 = reverseEnumMap(roleKindsFromProto)
	roleStatusesToProto              = reverseEnumMap(roleStatusesFromProto)
	promptKindsToProto               = reverseEnumMap(promptKindsFromProto)
	promptVersionStatusesToProto     = reverseEnumMap(promptVersionStatusesFromProto)
	agentRunStatusesToProto          = reverseEnumMap(agentRunStatusesFromProto)
	agentSessionStatusesFromProto    = reverseEnumMap(agentSessionStatusesToProto)
	agentSessionSnapshotKindsToProto = reverseEnumMap(agentSessionSnapshotKindsFromProto)
	acceptanceCheckKindsToProto      = reverseEnumMap(acceptanceCheckKindsFromProto)
	acceptanceStatusesToProto        = reverseEnumMap(acceptanceStatusesFromProto)
	agentActivityKindsToProto        = reverseEnumMap(agentActivityKindsFromProto)
	agentActivityStatusesToProto     = reverseEnumMap(agentActivityStatusesFromProto)
	humanGateStatusesToProto         = reverseEnumMap(humanGateStatusesFromProto)
	humanGateOutcomesToProto         = reverseEnumMap(humanGateOutcomesFromProto)
	selfDeployPlanStatusesToProto    = reverseEnumMap(selfDeployPlanStatusesFromProto)
	selfDeployPathCategoriesToProto  = reverseEnumMap(selfDeployPathCategoriesFromProto)
	selfDeployRuntimeJobTypesToProto = reverseEnumMap(selfDeployRuntimeJobTypesFromProto)
)

func ScopeTypeFromProto(value agentsv1.AgentScopeType) (enum.AgentScopeType, error) {
	return enumFromProto(value, scopeTypesFromProto)
}

func ScopeTypeToProto(value string) agentsv1.AgentScopeType {
	return enumToProto(enum.AgentScopeType(value), scopeTypesToProto, agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_UNSPECIFIED)
}

func FlowStatusFromProto(value agentsv1.FlowStatus) (enum.FlowStatus, error) {
	return enumFromProto(value, flowStatusesFromProto)
}

func OptionalFlowStatusFromProto(value *agentsv1.FlowStatus) (*enum.FlowStatus, error) {
	return optionalEnumFromProto(value, flowStatusesFromProto)
}

func FlowStatusToProto(value enum.FlowStatus) agentsv1.FlowStatus {
	return enumToProto(value, flowStatusesToProto, agentsv1.FlowStatus_FLOW_STATUS_UNSPECIFIED)
}

func FlowVersionStatusToProto(value enum.FlowVersionStatus) agentsv1.FlowVersionStatus {
	return enumToProto(value, flowVersionStatusesToProto, agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_UNSPECIFIED)
}

func StageTypeFromProto(value agentsv1.StageType) (enum.StageType, error) {
	return enumFromProto(value, stageTypesFromProto)
}

func StageTypeToProto(value enum.StageType) agentsv1.StageType {
	return enumToProto(value, stageTypesToProto, agentsv1.StageType_STAGE_TYPE_UNSPECIFIED)
}

func StageRoleBindingKindFromProto(value agentsv1.StageRoleBindingKind) (enum.StageRoleBindingKind, error) {
	return enumFromProto(value, stageRoleBindingKindsFromProto)
}

func StageRoleBindingKindToProto(value enum.StageRoleBindingKind) agentsv1.StageRoleBindingKind {
	return enumToProto(value, stageRoleBindingKindsToProto, agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_UNSPECIFIED)
}

func RoleKindFromProto(value agentsv1.RoleKind) (enum.RoleKind, error) {
	return enumFromProto(value, roleKindsFromProto)
}

func OptionalRoleKindFromProto(value *agentsv1.RoleKind) (*enum.RoleKind, error) {
	return optionalEnumFromProto(value, roleKindsFromProto)
}

func RoleKindToProto(value enum.RoleKind) agentsv1.RoleKind {
	return enumToProto(value, roleKindsToProto, agentsv1.RoleKind_ROLE_KIND_UNSPECIFIED)
}

func RoleStatusFromProto(value agentsv1.RoleStatus) (enum.RoleStatus, error) {
	return enumFromProto(value, roleStatusesFromProto)
}

func OptionalRoleStatusFromProto(value *agentsv1.RoleStatus) (*enum.RoleStatus, error) {
	return optionalEnumFromProto(value, roleStatusesFromProto)
}

func RoleStatusToProto(value enum.RoleStatus) agentsv1.RoleStatus {
	return enumToProto(value, roleStatusesToProto, agentsv1.RoleStatus_ROLE_STATUS_UNSPECIFIED)
}

func PromptKindFromProto(value agentsv1.PromptKind) (enum.PromptKind, error) {
	return enumFromProto(value, promptKindsFromProto)
}

func OptionalPromptKindFromProto(value *agentsv1.PromptKind) (*enum.PromptKind, error) {
	return optionalEnumFromProto(value, promptKindsFromProto)
}

func PromptKindToProto(value enum.PromptKind) agentsv1.PromptKind {
	return enumToProto(value, promptKindsToProto, agentsv1.PromptKind_PROMPT_KIND_UNSPECIFIED)
}

func PromptVersionStatusFromProto(value agentsv1.PromptVersionStatus) (enum.PromptVersionStatus, error) {
	return enumFromProto(value, promptVersionStatusesFromProto)
}

func OptionalPromptVersionStatusFromProto(value *agentsv1.PromptVersionStatus) (*enum.PromptVersionStatus, error) {
	return optionalEnumFromProto(value, promptVersionStatusesFromProto)
}

func PromptVersionStatusToProto(value enum.PromptVersionStatus) agentsv1.PromptVersionStatus {
	return enumToProto(value, promptVersionStatusesToProto, agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_UNSPECIFIED)
}

func AgentRunStatusFromProto(value agentsv1.AgentRunStatus) (enum.AgentRunStatus, error) {
	return enumFromProto(value, agentRunStatusesFromProto)
}

func OptionalAgentRunStatusFromProto(value *agentsv1.AgentRunStatus) (*enum.AgentRunStatus, error) {
	return optionalEnumFromProto(value, agentRunStatusesFromProto)
}

func AgentRunStatusToProto(value enum.AgentRunStatus) agentsv1.AgentRunStatus {
	return enumToProto(value, agentRunStatusesToProto, agentsv1.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED)
}

func AgentRunnerRunStateFromProto(value agentsv1.AgentRunnerRunState) (agentservice.RunnerRunState, error) {
	return enumFromProto(value, agentRunnerRunStatesFromProto)
}

func RuntimeObservationStateToProto(value agentservice.RuntimeObservationState) agentsv1.AgentRunRuntimeObservationState {
	return enumToProto(value, runtimeObservationStatesToProto, agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_UNSPECIFIED)
}

func RuntimeJobStatusToProto(value agentservice.RuntimeJobStatus) agentsv1.AgentRuntimeJobStatus {
	return enumToProto(value, runtimeJobStatusesToProto, agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_UNSPECIFIED)
}

func AgentSessionStatusToProto(value enum.AgentSessionStatus) agentsv1.AgentSessionStatus {
	return enumToProto(value, agentSessionStatusesToProto, agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_UNSPECIFIED)
}

func OptionalAgentSessionStatusFromProto(value *agentsv1.AgentSessionStatus) (*enum.AgentSessionStatus, error) {
	return optionalEnumFromProto(value, agentSessionStatusesFromProto)
}

func AgentSessionSnapshotKindFromProto(value agentsv1.AgentSessionSnapshotKind) (enum.AgentSessionSnapshotKind, error) {
	return enumFromProto(value, agentSessionSnapshotKindsFromProto)
}

func AgentSessionSnapshotKindToProto(value enum.AgentSessionSnapshotKind) agentsv1.AgentSessionSnapshotKind {
	return enumToProto(value, agentSessionSnapshotKindsToProto, agentsv1.AgentSessionSnapshotKind_AGENT_SESSION_SNAPSHOT_KIND_UNSPECIFIED)
}

func AcceptanceCheckKindFromProto(value agentsv1.AcceptanceCheckKind) (enum.AcceptanceCheckKind, error) {
	return enumFromProto(value, acceptanceCheckKindsFromProto)
}

func AcceptanceCheckKindToProto(value enum.AcceptanceCheckKind) agentsv1.AcceptanceCheckKind {
	return enumToProto(value, acceptanceCheckKindsToProto, agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_UNSPECIFIED)
}

func AcceptanceStatusFromProto(value agentsv1.AcceptanceStatus) (enum.AcceptanceStatus, error) {
	return enumFromProto(value, acceptanceStatusesFromProto)
}

func OptionalAcceptanceStatusFromProto(value *agentsv1.AcceptanceStatus) (*enum.AcceptanceStatus, error) {
	return optionalEnumFromProto(value, acceptanceStatusesFromProto)
}

func AcceptanceStatusToProto(value enum.AcceptanceStatus) agentsv1.AcceptanceStatus {
	return enumToProto(value, acceptanceStatusesToProto, agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_UNSPECIFIED)
}

func FollowUpIntentStatusToProto(value enum.FollowUpIntentStatus) agentsv1.FollowUpIntentStatus {
	return enumToProto(value, followUpIntentStatusesToProto, agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_UNSPECIFIED)
}

func AgentActivityKindFromProto(value agentsv1.AgentActivityKind) (enum.AgentActivityKind, error) {
	return enumFromProto(value, agentActivityKindsFromProto)
}

func OptionalAgentActivityKindFromProto(value *agentsv1.AgentActivityKind) (*enum.AgentActivityKind, error) {
	return optionalEnumFromProto(value, agentActivityKindsFromProto)
}

func AgentActivityKindToProto(value enum.AgentActivityKind) agentsv1.AgentActivityKind {
	return enumToProto(value, agentActivityKindsToProto, agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_UNSPECIFIED)
}

func AgentActivityStatusFromProto(value agentsv1.AgentActivityStatus) (enum.AgentActivityStatus, error) {
	return enumFromProto(value, agentActivityStatusesFromProto)
}

func OptionalAgentActivityStatusFromProto(value *agentsv1.AgentActivityStatus) (*enum.AgentActivityStatus, error) {
	return optionalEnumFromProto(value, agentActivityStatusesFromProto)
}

func AgentActivityStatusToProto(value enum.AgentActivityStatus) agentsv1.AgentActivityStatus {
	return enumToProto(value, agentActivityStatusesToProto, agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_UNSPECIFIED)
}

func HumanGateStatusFromProto(value agentsv1.HumanGateStatus) (enum.HumanGateStatus, error) {
	return enumFromProto(value, humanGateStatusesFromProto)
}

func OptionalHumanGateStatusFromProto(value *agentsv1.HumanGateStatus) (*enum.HumanGateStatus, error) {
	return optionalEnumFromProto(value, humanGateStatusesFromProto)
}

func HumanGateStatusToProto(value enum.HumanGateStatus) agentsv1.HumanGateStatus {
	return enumToProto(value, humanGateStatusesToProto, agentsv1.HumanGateStatus_HUMAN_GATE_STATUS_UNSPECIFIED)
}

func HumanGateOutcomeFromProto(value agentsv1.HumanGateOutcome) (enum.HumanGateOutcome, error) {
	return enumFromProto(value, humanGateOutcomesFromProto)
}

func OptionalHumanGateOutcomeFromProto(value *agentsv1.HumanGateOutcome) (*enum.HumanGateOutcome, error) {
	return optionalEnumFromProto(value, humanGateOutcomesFromProto)
}

func HumanGateOutcomeToProto(value enum.HumanGateOutcome) agentsv1.HumanGateOutcome {
	return enumToProto(value, humanGateOutcomesToProto, agentsv1.HumanGateOutcome_HUMAN_GATE_OUTCOME_UNSPECIFIED)
}

func SelfDeployPlanStatusFromProto(value agentsv1.SelfDeployPlanStatus) (enum.SelfDeployPlanStatus, error) {
	return enumFromProto(value, selfDeployPlanStatusesFromProto)
}

func OptionalSelfDeployPlanStatusFromProto(value *agentsv1.SelfDeployPlanStatus) (*enum.SelfDeployPlanStatus, error) {
	return optionalEnumFromProto(value, selfDeployPlanStatusesFromProto)
}

func SelfDeployPlanStatusToProto(value enum.SelfDeployPlanStatus) agentsv1.SelfDeployPlanStatus {
	return enumToProto(value, selfDeployPlanStatusesToProto, agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_UNSPECIFIED)
}

func SelfDeployPathCategoryFromProto(value agentsv1.SelfDeployPathCategory) (enum.SelfDeployPathCategory, error) {
	return enumFromProto(value, selfDeployPathCategoriesFromProto)
}

func SelfDeployPathCategoryToProto(value enum.SelfDeployPathCategory) agentsv1.SelfDeployPathCategory {
	return enumToProto(value, selfDeployPathCategoriesToProto, agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_UNSPECIFIED)
}

func SelfDeployRuntimeJobTypeFromProto(value runtimev1.JobType) (enum.SelfDeployRuntimeJobType, error) {
	return enumFromProto(value, selfDeployRuntimeJobTypesFromProto)
}

func SelfDeployRuntimeJobTypeToProto(value enum.SelfDeployRuntimeJobType) runtimev1.JobType {
	return enumToProto(value, selfDeployRuntimeJobTypesToProto, runtimev1.JobType_JOB_TYPE_UNSPECIFIED)
}

func enumFromProto[Proto comparable, Domain comparable](value Proto, values map[Proto]Domain) (Domain, error) {
	result, ok := values[value]
	if !ok {
		var zero Domain
		return zero, errs.ErrInvalidArgument
	}
	return result, nil
}

func optionalEnumFromProto[Proto comparable, Domain comparable](value *Proto, values map[Proto]Domain) (*Domain, error) {
	if value == nil {
		return nil, nil
	}
	result, err := enumFromProto(*value, values)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func enumToProto[Domain comparable, Proto comparable](value Domain, values map[Domain]Proto, fallback Proto) Proto {
	result, ok := values[value]
	if !ok {
		return fallback
	}
	return result
}

func reverseEnumMap[Proto comparable, Domain comparable](values map[Proto]Domain) map[Domain]Proto {
	result := make(map[Domain]Proto, len(values))
	for protoValue, domainValue := range values {
		result[domainValue] = protoValue
	}
	return result
}

type enumKV[Proto comparable, Domain comparable] struct {
	proto  Proto
	domain Domain
}

func enumPair[Proto comparable, Domain comparable](protoValue Proto, domainValue Domain) enumKV[Proto, Domain] {
	return enumKV[Proto, Domain]{proto: protoValue, domain: domainValue}
}

func enumMap[Proto comparable, Domain comparable](items ...enumKV[Proto, Domain]) map[Proto]Domain {
	result := make(map[Proto]Domain, len(items))
	for _, item := range items {
		result[item.proto] = item.domain
	}
	return result
}

func roleKindValues() map[agentsv1.RoleKind]enum.RoleKind {
	protoValues := []agentsv1.RoleKind{
		agentsv1.RoleKind_ROLE_KIND_WORKER,
		agentsv1.RoleKind_ROLE_KIND_REVIEWER,
		agentsv1.RoleKind_ROLE_KIND_GATEKEEPER,
		agentsv1.RoleKind_ROLE_KIND_MANAGER,
		agentsv1.RoleKind_ROLE_KIND_QA,
		agentsv1.RoleKind_ROLE_KIND_OPS,
		agentsv1.RoleKind_ROLE_KIND_CUSTOM,
	}
	domainValues := []enum.RoleKind{
		enum.RoleKindWorker,
		enum.RoleKindReviewer,
		enum.RoleKindGatekeeper,
		enum.RoleKindManager,
		enum.RoleKindQA,
		enum.RoleKindOps,
		enum.RoleKindCustom,
	}
	return pairedEnumValues(protoValues, domainValues)
}

func agentRunStatusValues() map[agentsv1.AgentRunStatus]enum.AgentRunStatus {
	values := make(map[agentsv1.AgentRunStatus]enum.AgentRunStatus, 7)
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_REQUESTED] = enum.AgentRunStatusRequested
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING] = enum.AgentRunStatusStarting
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING] = enum.AgentRunStatusRunning
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_WAITING] = enum.AgentRunStatusWaiting
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_COMPLETED] = enum.AgentRunStatusCompleted
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_FAILED] = enum.AgentRunStatusFailed
	values[agentsv1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED] = enum.AgentRunStatusCancelled
	return values
}

func agentRunnerRunStateValues() map[agentsv1.AgentRunnerRunState]agentservice.RunnerRunState {
	values := enumMap(
		enumPair(agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_QUEUED, agentservice.RunnerRunStateQueued),
		enumPair(agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_RUNNING, agentservice.RunnerRunStateRunning),
		enumPair(agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_COMPLETED, agentservice.RunnerRunStateCompleted),
		enumPair(agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_FAILED, agentservice.RunnerRunStateFailed),
	)
	values[agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_STARTED] = agentservice.RunnerRunStateStarted
	values[agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_CANCELLED] = agentservice.RunnerRunStateCancelled
	values[agentsv1.AgentRunnerRunState_AGENT_RUNNER_RUN_STATE_TIMED_OUT] = agentservice.RunnerRunStateTimedOut
	return values
}

func agentActivityKindValues() map[agentsv1.AgentActivityKind]enum.AgentActivityKind {
	protoValues := []agentsv1.AgentActivityKind{
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_LIFECYCLE,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_USE,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_RESULT,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_PERMISSION,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_PROVIDER_SIGNAL,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_RUNTIME_SIGNAL,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_CHECKPOINT,
		agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_OTHER,
	}
	domainValues := []enum.AgentActivityKind{
		enum.AgentActivityKindLifecycle,
		enum.AgentActivityKindToolUse,
		enum.AgentActivityKindToolResult,
		enum.AgentActivityKindPermission,
		enum.AgentActivityKindProviderSignal,
		enum.AgentActivityKindRuntimeSignal,
		enum.AgentActivityKindCheckpoint,
		enum.AgentActivityKindOther,
	}
	return pairedEnumValues(protoValues, domainValues)
}

func agentActivityStatusValues() map[agentsv1.AgentActivityStatus]enum.AgentActivityStatus {
	values := make(map[agentsv1.AgentActivityStatus]enum.AgentActivityStatus, 8)
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_PLANNED] = enum.AgentActivityStatusPlanned
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_STARTED] = enum.AgentActivityStatusStarted
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED] = enum.AgentActivityStatusSucceeded
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_FAILED] = enum.AgentActivityStatusFailed
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_DENIED] = enum.AgentActivityStatusDenied
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_WAITING] = enum.AgentActivityStatusWaiting
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_CANCELLED] = enum.AgentActivityStatusCancelled
	values[agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SKIPPED] = enum.AgentActivityStatusSkipped
	return values
}

func pairedEnumValues[Proto comparable, Domain comparable](protoValues []Proto, domainValues []Domain) map[Proto]Domain {
	values := make(map[Proto]Domain, len(protoValues))
	for idx, protoValue := range protoValues {
		values[protoValue] = domainValues[idx]
	}
	return values
}

func stageRoleBindingKindValues() map[agentsv1.StageRoleBindingKind]enum.StageRoleBindingKind {
	values := make(map[agentsv1.StageRoleBindingKind]enum.StageRoleBindingKind, 6)
	values[agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_EXECUTOR] = enum.StageRoleBindingKindExecutor
	values[agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_REVIEWER] = enum.StageRoleBindingKindReviewer
	values[agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_GATEKEEPER] = enum.StageRoleBindingKindGatekeeper
	values[agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_QA] = enum.StageRoleBindingKindQA
	values[agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_OBSERVER] = enum.StageRoleBindingKindObserver
	values[agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_CUSTOM] = enum.StageRoleBindingKindCustom
	return values
}

func acceptanceCheckKindValues() map[agentsv1.AcceptanceCheckKind]enum.AcceptanceCheckKind {
	protoValues := []agentsv1.AcceptanceCheckKind{
		agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_ARTIFACT,
		agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_WATERMARK,
		agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_POLICY,
		agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_ROLE_RESULT,
		agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_HUMAN_GATE,
		agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_FOLLOW_UP,
	}
	domainValues := []enum.AcceptanceCheckKind{
		enum.AcceptanceCheckKindArtifact,
		enum.AcceptanceCheckKindWatermark,
		enum.AcceptanceCheckKindPolicy,
		enum.AcceptanceCheckKindRoleResult,
		enum.AcceptanceCheckKindHumanGate,
		enum.AcceptanceCheckKindFollowUp,
	}
	values := make(map[agentsv1.AcceptanceCheckKind]enum.AcceptanceCheckKind, len(protoValues))
	for idx, protoValue := range protoValues {
		values[protoValue] = domainValues[idx]
	}
	return values
}
