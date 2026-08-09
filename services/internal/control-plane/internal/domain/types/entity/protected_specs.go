package entity

import (
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

// RoleDefinitionSpec отделяет reusable policy от исполняемого Agent.
type RoleDefinitionSpec struct {
	StableKey                      string                 `json:"stableKey"`
	Description                    string                 `json:"description"`
	Capabilities                   []string               `json:"capabilities"`
	AllowedTargetRoleDefinitionIDs []string               `json:"allowedTargetRoleDefinitionIds"`
	RoleImageRecipeID              string                 `json:"roleImageRecipeId,omitempty"`
	RoleImageRecipeVersion         uint64                 `json:"roleImageRecipeVersion,omitempty"`
	RoleImageRecipeSHA256          string                 `json:"roleImageRecipeSha256,omitempty"`
	Ownership                      ConfigurationOwnership `json:"ownership"`
}

func (RoleDefinitionSpec) Kind() enum.Kind { return enum.KindRoleDefinition }
func (spec RoleDefinitionSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}
func (spec RoleDefinitionSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil || len(spec.Description) > 4096 ||
		!validPermissions(spec.Capabilities, 64) ||
		!validUniqueIDs(spec.AllowedTargetRoleDefinitionIDs) ||
		spec.Ownership.Validate() != nil {
		return errors.New("role definition specification is invalid")
	}
	if (spec.RoleImageRecipeID == "") != (spec.RoleImageRecipeVersion == 0) ||
		(spec.RoleImageRecipeID == "") != (spec.RoleImageRecipeSHA256 == "") ||
		(spec.RoleImageRecipeID != "" && (value.ValidateID(spec.RoleImageRecipeID) != nil ||
			!validSHA256(spec.RoleImageRecipeSHA256))) {
		return errors.New("role definition image recipe reference is invalid")
	}
	return nil
}

// AgentSpec хранит только exact server-resolved references.
type AgentSpec struct {
	StableKey                 string                 `json:"stableKey"`
	RoleDefinitionID          string                 `json:"roleDefinitionId"`
	RoleDefinitionVersion     uint64                 `json:"roleDefinitionVersion"`
	RoleDefinitionSHA256      string                 `json:"roleDefinitionSha256"`
	InstructionSetID          string                 `json:"instructionSetId"`
	InstructionSetVersion     uint64                 `json:"instructionSetVersion"`
	InstructionSetSHA256      string                 `json:"instructionSetSha256"`
	ProviderPoolID            string                 `json:"providerPoolId"`
	ProviderPoolVersion       uint64                 `json:"providerPoolVersion"`
	ProviderPoolSHA256        string                 `json:"providerPoolSha256"`
	OwnerRoleSelector         string                 `json:"ownerRoleSelector,omitempty"`
	OwnerInstructionSelector  string                 `json:"ownerInstructionSelector,omitempty"`
	OwnerProviderPoolSelector string                 `json:"ownerProviderPoolSelector,omitempty"`
	RuntimeProfileRef         string                 `json:"runtimeProfileRef"`
	RuntimeProfileVersion     uint64                 `json:"runtimeProfileVersion"`
	RuntimeProfileSHA256      string                 `json:"runtimeProfileSha256"`
	Capabilities              []string               `json:"capabilities"`
	BotIdentityRef            string                 `json:"botIdentityRef,omitempty"`
	BotUsername               string                 `json:"botUsername,omitempty"`
	BotProviderRevision       uint64                 `json:"botProviderRevision,omitempty"`
	BotProviderGeneration     uint64                 `json:"botProviderGeneration,omitempty"`
	BotProviderTeamRef        string                 `json:"botProviderTeamRef,omitempty"`
	BotMaskedStatus           string                 `json:"botMaskedStatus,omitempty"`
	BotReceiptID              string                 `json:"botReceiptId,omitempty"`
	BotReceiptVersion         uint64                 `json:"botReceiptVersion,omitempty"`
	BotReceiptSHA256          string                 `json:"botReceiptSha256,omitempty"`
	Enabled                   bool                   `json:"enabled"`
	Ownership                 ConfigurationOwnership `json:"ownership"`
}

func (AgentSpec) Kind() enum.Kind                                     { return enum.KindAgent }
func (spec AgentSpec) ConfigurationOwnership() ConfigurationOwnership { return spec.Ownership }
func (spec AgentSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		value.ValidateID(spec.RoleDefinitionID) != nil || spec.RoleDefinitionVersion == 0 ||
		!validSHA256(spec.RoleDefinitionSHA256) ||
		value.ValidateID(spec.InstructionSetID) != nil || spec.InstructionSetVersion == 0 ||
		!validSHA256(spec.InstructionSetSHA256) ||
		value.ValidateID(spec.ProviderPoolID) != nil || spec.ProviderPoolVersion == 0 ||
		!validSHA256(spec.ProviderPoolSHA256) ||
		!validExternalRef(spec.RuntimeProfileRef) || spec.RuntimeProfileVersion == 0 ||
		!validSHA256(spec.RuntimeProfileSHA256) || !validPermissions(spec.Capabilities, 64) ||
		spec.Ownership.Validate() != nil {
		return errors.New("agent specification is invalid")
	}
	ownerForm := spec.OwnerRoleSelector != "" || spec.OwnerInstructionSelector != "" ||
		spec.OwnerProviderPoolSelector != ""
	if ownerForm && (value.ValidateStableKey(spec.OwnerRoleSelector) != nil ||
		value.ValidateStableKey(spec.OwnerInstructionSelector) != nil ||
		value.ValidateStableKey(spec.OwnerProviderPoolSelector) != nil) {
		return errors.New("agent owner form selection is invalid")
	}
	botBound := spec.BotIdentityRef != "" || spec.BotUsername != "" || spec.BotProviderRevision != 0 ||
		spec.BotProviderGeneration != 0 || spec.BotProviderTeamRef != "" ||
		spec.BotMaskedStatus != "" || spec.BotReceiptID != "" || spec.BotReceiptVersion != 0 || spec.BotReceiptSHA256 != ""
	if botBound && (!validExternalRef(spec.BotIdentityRef) || !validProviderUsername(spec.BotUsername) ||
		spec.BotProviderRevision == 0 || spec.BotProviderGeneration == 0 || !validExternalRef(spec.BotProviderTeamRef) ||
		(spec.BotMaskedStatus != "AVAILABLE" && spec.BotMaskedStatus != "REVOKED") ||
		value.ValidateID(spec.BotReceiptID) != nil || spec.BotReceiptVersion == 0 || !validSHA256(spec.BotReceiptSHA256)) {
		return errors.New("agent bot identity specification is invalid")
	}
	return nil
}

// AgentAssignmentSpec — server-owned Agent↔Workspace aggregate.
type AgentAssignmentSpec struct {
	AgentID              string `json:"agentId"`
	AgentVersion         uint64 `json:"agentVersion"`
	AgentSHA256          string `json:"agentSha256"`
	WorkspaceID          string `json:"workspaceId"`
	WorkspaceVersion     uint64 `json:"workspaceVersion"`
	WorkspaceSHA256      string `json:"workspaceSha256"`
	RoomID               string `json:"roomId,omitempty"`
	RootActorID          string `json:"rootActorId"`
	AssignmentGeneration uint64 `json:"assignmentGeneration"`
}

func (AgentAssignmentSpec) Kind() enum.Kind { return enum.KindAgentAssignment }
func (spec AgentAssignmentSpec) Validate() error {
	if value.ValidateID(spec.AgentID) != nil || spec.AgentVersion == 0 || !validSHA256(spec.AgentSHA256) ||
		value.ValidateID(spec.WorkspaceID) != nil || spec.WorkspaceVersion == 0 || !validSHA256(spec.WorkspaceSHA256) ||
		(spec.RoomID != "" && value.ValidateID(spec.RoomID) != nil) ||
		value.ValidateID(spec.RootActorID) != nil || spec.AssignmentGeneration == 0 {
		return errors.New("agent assignment specification is invalid")
	}
	return nil
}

// InstructionSetSpec содержит одну текущую immutable content version.
type InstructionSetSpec struct {
	StableKey               string                       `json:"stableKey"`
	Locale                  string                       `json:"locale"`
	CurrentVersion          uint64                       `json:"currentVersion"`
	PublishedVersion        uint64                       `json:"publishedVersion,omitempty"`
	Content                 string                       `json:"content"`
	ContentSHA256           string                       `json:"contentSha256"`
	VersionState            string                       `json:"versionState"`
	ValidationSHA256        string                       `json:"validationSha256,omitempty"`
	ValidationSucceeded     bool                         `json:"validationSucceeded"`
	ValidatedContentVersion uint64                       `json:"validatedContentVersion,omitempty"`
	ValidatedContentSHA256  string                       `json:"validatedContentSha256,omitempty"`
	ValidationErrors        []InstructionValidationError `json:"validationErrors,omitempty"`
	RollbackOfVersion       uint64                       `json:"rollbackOfVersion,omitempty"`
	ContentArtifactID       string                       `json:"contentArtifactId"`
	ContentArtifactVersion  uint64                       `json:"contentArtifactVersion"`
	Ownership               ConfigurationOwnership       `json:"ownership"`
}

type InstructionValidationError struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Line    uint32 `json:"line,omitempty"`
	Column  uint32 `json:"column,omitempty"`
	Message string `json:"message"`
}

func (InstructionSetSpec) Kind() enum.Kind                                     { return enum.KindInstructionSet }
func (spec InstructionSetSpec) ConfigurationOwnership() ConfigurationOwnership { return spec.Ownership }
func (spec InstructionSetSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		(spec.Locale != "ru" && spec.Locale != "en") || spec.CurrentVersion == 0 ||
		len(spec.Content) == 0 || len(spec.Content) > 262144 ||
		digestText(spec.Content) != spec.ContentSHA256 || value.ValidateID(spec.ContentArtifactID) != nil ||
		spec.ContentArtifactVersion == 0 || spec.Ownership.Validate() != nil {
		return errors.New("instruction set specification is invalid")
	}
	switch spec.VersionState {
	case "DRAFT":
		if spec.ValidationSHA256 != "" || spec.ValidationSucceeded || spec.ValidatedContentVersion != 0 ||
			spec.ValidatedContentSHA256 != "" || len(spec.ValidationErrors) != 0 || spec.PublishedVersion >= spec.CurrentVersion {
			return errors.New("draft instruction version is invalid")
		}
	case "VALIDATED", "REJECTED":
		if !validSHA256(spec.ValidationSHA256) || spec.ValidatedContentVersion != spec.CurrentVersion ||
			spec.ValidatedContentSHA256 != spec.ContentSHA256 || spec.PublishedVersion >= spec.CurrentVersion ||
			(spec.VersionState == "VALIDATED") != spec.ValidationSucceeded ||
			(spec.ValidationSucceeded && len(spec.ValidationErrors) != 0) ||
			(!spec.ValidationSucceeded && len(spec.ValidationErrors) == 0) {
			return errors.New("validated instruction version is invalid")
		}
	case "PUBLISHED":
		if !validSHA256(spec.ValidationSHA256) || !spec.ValidationSucceeded || len(spec.ValidationErrors) != 0 ||
			spec.ValidatedContentVersion != spec.CurrentVersion || spec.ValidatedContentSHA256 != spec.ContentSHA256 ||
			spec.PublishedVersion != spec.CurrentVersion {
			return errors.New("published instruction version is invalid")
		}
	case "ARCHIVED":
		if spec.PublishedVersion > spec.CurrentVersion {
			return errors.New("archived instruction version is invalid")
		}
	default:
		return errors.New("instruction version state is invalid")
	}
	for _, validationError := range spec.ValidationErrors {
		if value.ValidateStableKey(validationError.Code) != nil || validationError.Field != "content" ||
			len(validationError.Message) == 0 || len(validationError.Message) > 256 {
			return errors.New("instruction validation error is invalid")
		}
	}
	if spec.RollbackOfVersion >= spec.CurrentVersion {
		return errors.New("instruction rollback lineage is invalid")
	}
	return nil
}

// ProviderConnectionReferenceSpec намеренно исключает credential values и provider payload.
type ProviderConnectionReferenceSpec struct {
	StableKey                string    `json:"stableKey"`
	Provider                 string    `json:"provider"`
	ServerReference          string    `json:"serverReference"`
	ReferenceVersion         uint64    `json:"referenceVersion"`
	ReferenceGeneration      uint64    `json:"referenceGeneration"`
	ReferenceSHA256          string    `json:"referenceSha256"`
	MaskedLabel              string    `json:"maskedLabel"`
	MaskedStatus             string    `json:"maskedStatus"`
	Capabilities             []string  `json:"capabilities"`
	Eligible                 bool      `json:"eligible"`
	ObservedAt               time.Time `json:"observedAt"`
	ReceiptID                string    `json:"receiptId"`
	ReceiptVersion           uint64    `json:"receiptVersion"`
	ReceiptSHA256            string    `json:"receiptSha256"`
	CredentialBindingID      string    `json:"credentialBindingId"`
	CredentialBindingVersion uint64    `json:"credentialBindingVersion"`
	CredentialBindingSHA256  string    `json:"credentialBindingSha256"`
	ObservedUsage            uint64    `json:"observedUsage"`
	ObservedLimit            uint64    `json:"observedLimit"`
	ObservationRevision      uint64    `json:"observationRevision"`
	ObservationExpiresAt     time.Time `json:"observationExpiresAt"`
	WindowDurationSeconds    uint64    `json:"windowDurationSeconds"`
	ResetsAt                 time.Time `json:"resetsAt"`
	ObservationSHA256        string    `json:"observationSha256"`
}

func (ProviderConnectionReferenceSpec) Kind() enum.Kind { return enum.KindProviderReference }
func (spec ProviderConnectionReferenceSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil || value.ValidateStableKey(spec.Provider) != nil ||
		!validExternalRef(spec.ServerReference) || spec.ReferenceVersion == 0 || spec.ReferenceGeneration == 0 ||
		!validSHA256(spec.ReferenceSHA256) || len(spec.MaskedLabel) == 0 || len(spec.MaskedLabel) > 256 ||
		!validBoundedKeys(spec.Capabilities, 64) || spec.ObservedAt.IsZero() ||
		value.ValidateID(spec.ReceiptID) != nil || spec.ReceiptVersion == 0 ||
		!validSHA256(spec.ReceiptSHA256) || value.ValidateID(spec.CredentialBindingID) != nil ||
		spec.CredentialBindingVersion == 0 || !validSHA256(spec.CredentialBindingSHA256) {
		return errors.New("provider connection reference specification is invalid")
	}
	if spec.Eligible && (spec.ObservedLimit == 0 || spec.ObservedUsage > spec.ObservedLimit ||
		spec.ObservationRevision == 0 || !spec.ObservationExpiresAt.After(spec.ObservedAt) ||
		spec.WindowDurationSeconds == 0 || spec.ResetsAt.IsZero() || !validSHA256(spec.ObservationSHA256)) {
		return errors.New("provider capacity observation is invalid")
	}
	switch spec.MaskedStatus {
	case "AVAILABLE", "DEGRADED":
		if !spec.Eligible {
			return errors.New("eligible provider connection status is invalid")
		}
	case "INELIGIBLE", "ARCHIVED":
		if spec.Eligible {
			return errors.New("ineligible provider connection status is invalid")
		}
	default:
		return errors.New("provider connection status is invalid")
	}
	return nil
}

func validProviderUsername(username string) bool {
	if len(username) < 1 || len(username) > 64 {
		return false
	}
	for _, symbol := range username {
		if (symbol < 'a' || symbol > 'z') && (symbol < '0' || symbol > '9') && symbol != '-' && symbol != '_' && symbol != '.' {
			return false
		}
	}
	return true
}

type ProviderPoolBinding struct {
	ProviderConnectionReferenceID string    `json:"providerConnectionReferenceId"`
	ProviderConnectionStableKey   string    `json:"providerConnectionStableKey"`
	ReferenceVersion              uint64    `json:"referenceVersion"`
	ReferenceSHA256               string    `json:"referenceSha256"`
	Weight                        uint32    `json:"weight"`
	Eligible                      bool      `json:"eligible"`
	MaskedStatus                  string    `json:"maskedStatus"`
	ObservedUsage                 uint64    `json:"observedUsage"`
	ObservedLimit                 uint64    `json:"observedLimit"`
	ObservationRevision           uint64    `json:"observationRevision"`
	ObservedAt                    time.Time `json:"observedAt"`
	ObservationExpiresAt          time.Time `json:"observationExpiresAt"`
	ObservationSHA256             string    `json:"observationSha256"`
	WindowDurationSeconds         uint64    `json:"windowDurationSeconds"`
	ResetsAt                      time.Time `json:"resetsAt"`
}

// ProviderPoolSpec хранит immutable eligibility snapshot.
type ProviderPoolSpec struct {
	StableKey                 string                 `json:"stableKey"`
	Policy                    string                 `json:"policy"`
	PolicyRevision            uint64                 `json:"policyRevision"`
	ObservationMaxAge         time.Duration          `json:"observationMaxAge"`
	Bindings                  []ProviderPoolBinding  `json:"bindings"`
	EligibilitySnapshotSHA256 string                 `json:"eligibilitySnapshotSha256"`
	Ownership                 ConfigurationOwnership `json:"ownership"`
}

func (ProviderPoolSpec) Kind() enum.Kind                                     { return enum.KindProviderPool }
func (spec ProviderPoolSpec) ConfigurationOwnership() ConfigurationOwnership { return spec.Ownership }
func (spec ProviderPoolSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		(spec.Policy != "least_used" && spec.Policy != "weighted") || spec.PolicyRevision == 0 ||
		spec.ObservationMaxAge < time.Minute || spec.ObservationMaxAge > 24*time.Hour ||
		len(spec.Bindings) == 0 || len(spec.Bindings) > 32 ||
		!validSHA256(spec.EligibilitySnapshotSHA256) || spec.Ownership.Validate() != nil {
		return errors.New("provider pool specification is invalid")
	}
	ids := make([]string, 0, len(spec.Bindings))
	for _, binding := range spec.Bindings {
		if value.ValidateID(binding.ProviderConnectionReferenceID) != nil || binding.ReferenceVersion == 0 ||
			value.ValidateStableKey(binding.ProviderConnectionStableKey) != nil ||
			!validSHA256(binding.ReferenceSHA256) || binding.Weight == 0 || binding.Weight > 10000 ||
			!binding.Eligible || (binding.MaskedStatus != "AVAILABLE" && binding.MaskedStatus != "DEGRADED") {
			return errors.New("provider pool binding is invalid")
		}
		if binding.ObservedLimit == 0 || binding.ObservedUsage > binding.ObservedLimit || binding.ObservationRevision == 0 ||
			binding.ObservedAt.IsZero() || !binding.ObservationExpiresAt.After(binding.ObservedAt) || !validSHA256(binding.ObservationSHA256) ||
			binding.WindowDurationSeconds == 0 || binding.ResetsAt.IsZero() {
			return errors.New("provider pool capacity observation is invalid")
		}
		ids = append(ids, binding.ProviderConnectionReferenceID)
	}
	if !validUniqueIDs(ids) {
		return errors.New("provider pool binding is duplicated")
	}
	return nil
}

type WorkspaceBackupMember struct {
	SourceExecutionID     string `json:"sourceExecutionId"`
	WorkspaceID           string `json:"workspaceId"`
	WorkspaceVersion      uint64 `json:"workspaceVersion"`
	WorkspaceSHA256       string `json:"workspaceSha256"`
	SessionID             string `json:"sessionId"`
	SourceVersion         uint64 `json:"sourceVersion"`
	RuntimeRevisionSHA256 string `json:"runtimeRevisionSha256"`
	ImmutableInputSHA256  string `json:"immutableInputSha256"`
	ArchiveSHA256         string `json:"archiveSha256"`
	ProvenanceSHA256      string `json:"provenanceSha256"`
}

// WorkspaceBackupSpec описывает immutable membership полного envelope.
type WorkspaceBackupSpec struct {
	Scope              string                  `json:"scope"`
	ScopeID            string                  `json:"scopeId,omitempty"`
	Members            []WorkspaceBackupMember `json:"members"`
	MembershipSHA256   string                  `json:"membershipSha256"`
	BackupState        string                  `json:"backupState"`
	Attempt            uint32                  `json:"attempt"`
	Generation         uint64                  `json:"generation"`
	RevokedGeneration  uint64                  `json:"revokedGeneration"`
	TerminalReasonCode string                  `json:"terminalReasonCode,omitempty"`
	RetainUntil        time.Time               `json:"retainUntil"`
}

func (WorkspaceBackupSpec) Kind() enum.Kind { return enum.KindWorkspaceBackup }
func (spec WorkspaceBackupSpec) Validate() error {
	if (spec.Scope != "WORKSPACE" && spec.Scope != "ALL_WORKSPACES") ||
		(spec.Scope == "WORKSPACE" && value.ValidateID(spec.ScopeID) != nil) ||
		(spec.Scope == "ALL_WORKSPACES" && spec.ScopeID != "") || len(spec.Members) == 0 ||
		!validSHA256(spec.MembershipSHA256) || spec.Attempt == 0 || spec.Generation == 0 ||
		spec.RevokedGeneration > spec.Generation || spec.RetainUntil.IsZero() ||
		len(spec.TerminalReasonCode) > 128 {
		return errors.New("workspace backup specification is invalid")
	}
	switch spec.BackupState {
	case "VERIFYING":
		if spec.TerminalReasonCode != "" || spec.RevokedGeneration >= spec.Generation {
			return errors.New("nonterminal workspace backup reason is invalid")
		}
	case "AVAILABLE":
		if spec.TerminalReasonCode != "" || spec.RevokedGeneration != spec.Generation {
			return errors.New("available workspace backup generation is invalid")
		}
	case "FAILED", "CANCELLED", "EXPIRED":
		if value.ValidateStableKey(spec.TerminalReasonCode) != nil || spec.RevokedGeneration != spec.Generation {
			return errors.New("terminal workspace backup is invalid")
		}
	default:
		return errors.New("workspace backup state is invalid")
	}
	ids := make([]string, 0, len(spec.Members))
	for _, member := range spec.Members {
		if value.ValidateID(member.SourceExecutionID) != nil || value.ValidateID(member.WorkspaceID) != nil || member.WorkspaceVersion == 0 ||
			!validSHA256(member.WorkspaceSHA256) || value.ValidateID(member.SessionID) != nil ||
			member.SourceVersion == 0 || !validSHA256(member.RuntimeRevisionSHA256) ||
			!validSHA256(member.ImmutableInputSHA256) || !validSHA256(member.ArchiveSHA256) ||
			!validSHA256(member.ProvenanceSHA256) {
			return errors.New("workspace backup member is invalid")
		}
		ids = append(ids, member.SessionID)
	}
	slices.Sort(ids)
	for index := 1; index < len(ids); index++ {
		if ids[index] == ids[index-1] {
			return errors.New("workspace backup member is duplicated")
		}
	}
	return nil
}

type WorkspaceRestoreMember struct {
	SourceExecutionID      string `json:"sourceExecutionId"`
	WorkspaceID            string `json:"workspaceId"`
	SourceSessionID        string `json:"sourceSessionId"`
	TargetTurnID           string `json:"targetTurnId"`
	TargetTurnVersion      uint64 `json:"targetTurnVersion"`
	TargetAttempt          uint32 `json:"targetAttempt"`
	RuntimeRevisionID      string `json:"runtimeRevisionId"`
	RuntimeRevisionVersion uint64 `json:"runtimeRevisionVersion"`
	ImmutableInputSHA256   string `json:"immutableInputSha256"`
	GrantSHA256            string `json:"grantSha256"`
	State                  string `json:"state"`
}

// WorkspaceRestoreSpec связывает full-envelope fresh attempt и revoke watermark.
type WorkspaceRestoreSpec struct {
	BackupID           string                   `json:"backupId"`
	BackupVersion      uint64                   `json:"backupVersion"`
	MembershipSHA256   string                   `json:"membershipSha256"`
	Members            []WorkspaceRestoreMember `json:"members"`
	RestoreState       string                   `json:"restoreState"`
	Attempt            uint32                   `json:"attempt"`
	Generation         uint64                   `json:"generation"`
	RevokedGeneration  uint64                   `json:"revokedGeneration"`
	Partial            bool                     `json:"partial"`
	TerminalReasonCode string                   `json:"terminalReasonCode,omitempty"`
}

func (WorkspaceRestoreSpec) Kind() enum.Kind { return enum.KindWorkspaceRestore }
func (spec WorkspaceRestoreSpec) Validate() error {
	if value.ValidateID(spec.BackupID) != nil || spec.BackupVersion == 0 ||
		!validSHA256(spec.MembershipSHA256) || len(spec.Members) == 0 || spec.Attempt == 0 ||
		spec.Generation == 0 || spec.RevokedGeneration > spec.Generation || spec.Partial ||
		len(spec.TerminalReasonCode) > 128 {
		return errors.New("workspace restore specification is invalid")
	}
	switch spec.RestoreState {
	case "QUEUED", "RUNNING":
		if spec.TerminalReasonCode != "" || spec.RevokedGeneration >= spec.Generation {
			return errors.New("workspace restore reason is invalid")
		}
	case "SUCCEEDED":
		if spec.TerminalReasonCode != "" || spec.RevokedGeneration != spec.Generation {
			return errors.New("succeeded workspace restore generation is invalid")
		}
	case "FAILED", "CANCELLED", "EXPIRED":
		if value.ValidateStableKey(spec.TerminalReasonCode) != nil || spec.RevokedGeneration != spec.Generation {
			return errors.New("terminal workspace restore is invalid")
		}
	default:
		return errors.New("workspace restore state is invalid")
	}
	for _, member := range spec.Members {
		if value.ValidateID(member.SourceExecutionID) != nil || value.ValidateID(member.WorkspaceID) != nil ||
			value.ValidateID(member.SourceSessionID) != nil || value.ValidateID(member.TargetTurnID) != nil ||
			member.TargetTurnVersion == 0 || member.TargetAttempt == 0 ||
			value.ValidateID(member.RuntimeRevisionID) != nil || member.RuntimeRevisionVersion == 0 ||
			!validSHA256(member.ImmutableInputSHA256) || !validSHA256(member.GrantSHA256) ||
			(member.State != "QUEUED" && member.State != "RUNNING" && member.State != "SUCCEEDED" &&
				member.State != "FAILED" && member.State != "CANCELLED" && member.State != "EXPIRED") {
			return errors.New("workspace restore member is invalid")
		}
	}
	return nil
}

// WorkspaceMattermostMappingSpec хранит только refs и receipt digest.
type WorkspaceMattermostMappingSpec struct {
	WorkspaceID              string    `json:"workspaceId"`
	WorkspaceVersion         uint64    `json:"workspaceVersion"`
	WorkspaceSHA256          string    `json:"workspaceSha256"`
	ProviderTeamRef          string    `json:"providerTeamRef"`
	ProviderReceiptID        string    `json:"providerReceiptId"`
	ProviderReceiptVersion   uint64    `json:"providerReceiptVersion"`
	ProviderReceiptSHA256    string    `json:"providerReceiptSha256"`
	ProviderEffectVersion    uint64    `json:"providerEffectVersion"`
	ProviderEffectGeneration uint64    `json:"providerEffectGeneration"`
	MappingGeneration        uint64    `json:"mappingGeneration"`
	MappingState             string    `json:"mappingState"`
	ProviderObservedAt       time.Time `json:"providerObservedAt"`
}

func (WorkspaceMattermostMappingSpec) Kind() enum.Kind { return enum.KindWorkspaceMapping }
func (spec WorkspaceMattermostMappingSpec) Validate() error {
	if value.ValidateID(spec.WorkspaceID) != nil || spec.WorkspaceVersion == 0 ||
		!validSHA256(spec.WorkspaceSHA256) || !validExternalRef(spec.ProviderTeamRef) ||
		value.ValidateID(spec.ProviderReceiptID) != nil || spec.ProviderReceiptVersion == 0 ||
		!validSHA256(spec.ProviderReceiptSHA256) || spec.ProviderEffectVersion == 0 ||
		spec.ProviderEffectGeneration == 0 || spec.MappingGeneration == 0 ||
		(spec.MappingState != "BOUND" && spec.MappingState != "UNLINKED") ||
		spec.ProviderObservedAt.IsZero() {
		return errors.New("workspace Mattermost mapping specification is invalid")
	}
	return nil
}
