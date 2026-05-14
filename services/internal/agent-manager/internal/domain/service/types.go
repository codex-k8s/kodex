package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

type CreateFlowInput struct {
	Meta          value.CommandMeta
	Scope         value.ScopeRef
	Slug          string
	DisplayName   []value.LocalizedText
	Description   []value.LocalizedText
	IconObjectURI string
}

type UpdateFlowInput struct {
	Meta          value.CommandMeta
	FlowID        uuid.UUID
	DisplayName   []value.LocalizedText
	Description   []value.LocalizedText
	IconObjectURI string
	Status        enum.FlowStatus
}

type CreateFlowVersionInput struct {
	Meta             value.CommandMeta
	FlowID           uuid.UUID
	SourceRef        string
	DefinitionDigest string
	Stages           []StageInput
	Transitions      []StageTransitionInput
	RoleBindings     []StageRoleBindingInput
}

type StageInput struct {
	Slug                  string
	StageType             enum.StageType
	DisplayName           []value.LocalizedText
	IconObjectURI         string
	RequiredArtifactsJSON []byte
	AcceptancePolicyJSON  []byte
	Position              int32
}

type StageTransitionInput struct {
	FromStageSlug *string
	ToStageSlug   string
	ConditionJSON []byte
	FollowUpType  string
	Position      int32
}

type StageRoleBindingInput struct {
	StageSlug             string
	RoleProfileID         uuid.UUID
	BindingKind           enum.StageRoleBindingKind
	LaunchPolicyJSON      []byte
	RequiredForAcceptance bool
}

type ActivateFlowVersionInput struct {
	Meta          value.CommandMeta
	FlowVersionID uuid.UUID
}

type CreateRoleProfileInput struct {
	Meta                     value.CommandMeta
	Scope                    value.ScopeRef
	Slug                     string
	DisplayName              []value.LocalizedText
	IconObjectURI            string
	RoleKind                 enum.RoleKind
	RuntimeProfile           string
	AllowedMCPTools          []string
	ProviderAccountPolicyRef string
}

type UpdateRoleProfileInput struct {
	Meta                     value.CommandMeta
	RoleProfileID            uuid.UUID
	DisplayName              []value.LocalizedText
	IconObjectURI            string
	RoleKind                 enum.RoleKind
	RuntimeProfile           string
	AllowedMCPTools          []string
	ProviderAccountPolicyRef string
	Status                   enum.RoleStatus
}

type CreatePromptTemplateInput struct {
	Meta          value.CommandMeta
	RoleProfileID uuid.UUID
	PromptKind    enum.PromptKind
}

type CreatePromptTemplateVersionInput struct {
	Meta           value.CommandMeta
	RoleProfileID  uuid.UUID
	PromptKind     enum.PromptKind
	SourceRef      string
	TemplateObject value.ObjectRef
	TemplateDigest string
}

type ActivatePromptTemplateVersionInput struct {
	Meta                    value.CommandMeta
	PromptTemplateVersionID uuid.UUID
}

type FlowList = query.FlowFilter
type RoleProfileList = query.RoleProfileFilter
type PromptTemplateList = query.PromptTemplateFilter
type PromptTemplateVersionList = query.PromptTemplateVersionFilter

type FlowVersionResult struct {
	FlowVersion entity.FlowVersion
	Flow        entity.Flow
}
