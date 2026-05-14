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

type CommandAggregateType string

const (
	CommandAggregateTypeFlow                  CommandAggregateType = "flow"
	CommandAggregateTypeFlowVersion           CommandAggregateType = "flow_version"
	CommandAggregateTypeRoleProfile           CommandAggregateType = "role_profile"
	CommandAggregateTypePromptTemplate        CommandAggregateType = "prompt_template"
	CommandAggregateTypePromptTemplateVersion CommandAggregateType = "prompt_template_version"
)
