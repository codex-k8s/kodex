package service

import (
	"time"

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

type StartAgentSessionInput struct {
	Meta                value.CommandMeta
	Scope               value.ScopeRef
	ProviderWorkItemRef string
	FlowVersionID       *uuid.UUID
	CurrentStageID      *uuid.UUID
	CreatedByActorRef   string
}

type StartAgentRunInput struct {
	Meta                    value.CommandMeta
	SessionID               uuid.UUID
	FlowVersionID           *uuid.UUID
	StageID                 *uuid.UUID
	RoleProfileID           uuid.UUID
	PromptTemplateVersionID uuid.UUID
	ProviderTarget          value.ProviderTargetRef
	GuidanceSelectionHints  []value.GuidanceSelectionHint
}

const (
	RuntimeModeFullEnv = "full_env"

	WorkspaceSourceKindCode             = "code"
	WorkspaceSourceKindDocumentation    = "documentation"
	WorkspaceSourceKindGuidancePackage  = "guidance_package"
	WorkspaceSourceKindGeneratedContext = "generated_context"

	WorkspaceSourceAccessRead  = "read"
	WorkspaceSourceAccessWrite = "write"
)

type WorkspacePolicySnapshot struct {
	ProjectID             uuid.UUID
	CodeSources           []WorkspaceCodeSource
	DocumentationSources  []WorkspaceDocumentationSource
	GuidancePackageRefs   []string
	ActivePolicyOverrides []PolicyOverrideRef
	PolicyVersion         int64
}

type WorkspaceCodeSource struct {
	RepositoryID  uuid.UUID
	Provider      string
	ProviderOwner string
	ProviderName  string
	DefaultBranch string
	LocalPath     string
	AccessMode    string
}

type WorkspaceDocumentationSource struct {
	DocumentationSourceID uuid.UUID
	RepositoryID          *uuid.UUID
	ScopeType             string
	ScopeID               string
	LocalPath             string
	AccessMode            string
}

type PolicyOverrideRef struct {
	ID string
}

type RuntimeWorkspacePolicy struct {
	ProjectID               uuid.UUID
	PolicyDigest            string
	PolicyVersion           int64
	Sources                 []RuntimeWorkspaceSource
	ActivePolicyOverrideIDs []string
}

type RuntimeWorkspaceSource struct {
	SourceID      string
	Kind          string
	RepositoryID  *uuid.UUID
	Provider      string
	ProviderOwner string
	ProviderName  string
	SourceRef     string
	CommitSHA     string
	LocalPath     string
	AccessMode    string
	Digest        string
	MetadataJSON  string
}

type RuntimePlacementConstraints struct {
	ProjectID             uuid.UUID
	RepositoryIDs         []uuid.UUID
	ServiceKeys           []string
	RuntimeProfile        string
	PreferredFleetScopeID *uuid.UUID
	RequiredCapabilities  []string
	MetadataJSON          string
}

type RuntimePreparationInput struct {
	Meta                 value.CommandMeta
	AgentRunID           uuid.UUID
	RuntimeProfile       string
	RuntimeMode          string
	WorkspacePolicy      RuntimeWorkspacePolicy
	PlacementConstraints RuntimePlacementConstraints
}

type RuntimePreparationResult struct {
	SlotRef                    string
	WorkspaceRef               string
	ContextRef                 string
	MaterializationFingerprint string
	DiagnosticSummary          string
}

type RecordRunStateInput struct {
	Meta           value.CommandMeta
	RunID          uuid.UUID
	Status         enum.AgentRunStatus
	RuntimeContext *value.RuntimeContextRef
	ProviderTarget *value.ProviderTargetRef
	ResultSummary  *string
	FailureCode    *string
	ReasonCode     *string
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

type RecordSessionStateSnapshotInput struct {
	Meta         value.CommandMeta
	SessionID    uuid.UUID
	RunID        *uuid.UUID
	SnapshotKind enum.AgentSessionSnapshotKind
	TurnIndex    *int64
	Object       value.ObjectRef
	CapturedAt   time.Time
}

type SessionSnapshotResult struct {
	Snapshot entity.AgentSessionStateSnapshot
	Session  entity.AgentSession
}

type AgentRunList = query.AgentRunFilter
