package grpc

import (
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func roleDefinitionFromProto(value *controlplanev1.RoleDefinitionSpec) entity.RoleDefinitionSpec {
	return entity.RoleDefinitionSpec{
		StableKey: value.GetStableKey(), Description: value.GetDescription(),
		Capabilities:                   value.GetCapabilities(),
		AllowedTargetRoleDefinitionIDs: value.GetAllowedTargetRoleDefinitionIds(),
		RoleImageRecipeID:              value.GetRoleImageRecipeId(),
		RoleImageRecipeVersion:         value.GetRoleImageRecipeVersion(),
		RoleImageRecipeSHA256:          value.GetRoleImageRecipeSha256(),
		Ownership:                      configurationOwnershipFromProto(value.GetOwnership()),
	}
}

func roleDefinitionToProto(value entity.RoleDefinitionSpec) *controlplanev1.RoleDefinitionSpec {
	return &controlplanev1.RoleDefinitionSpec{
		StableKey: value.StableKey, Description: value.Description, Capabilities: value.Capabilities,
		AllowedTargetRoleDefinitionIds: value.AllowedTargetRoleDefinitionIDs,
		RoleImageRecipeId:              value.RoleImageRecipeID, RoleImageRecipeVersion: value.RoleImageRecipeVersion,
		RoleImageRecipeSha256: value.RoleImageRecipeSHA256,
		Ownership:             configurationOwnershipToProto(value.Ownership),
	}
}

func agentFromProto(value *controlplanev1.AgentSpec) entity.AgentSpec {
	return entity.AgentSpec{
		StableKey: value.GetStableKey(), RoleDefinitionID: value.GetRoleDefinitionId(),
		RoleDefinitionVersion: value.GetRoleDefinitionVersion(), RoleDefinitionSHA256: value.GetRoleDefinitionSha256(),
		InstructionSetID: value.GetInstructionSetId(), InstructionSetVersion: value.GetInstructionSetVersion(),
		InstructionSetSHA256: value.GetInstructionSetSha256(), ProviderPoolID: value.GetProviderPoolId(),
		ProviderPoolVersion: value.GetProviderPoolVersion(), ProviderPoolSHA256: value.GetProviderPoolSha256(),
		RuntimeProfileRef: value.GetRuntimeProfileRef(), RuntimeProfileVersion: value.GetRuntimeProfileVersion(),
		RuntimeProfileSHA256: value.GetRuntimeProfileSha256(), Capabilities: value.GetCapabilities(),
		BotIdentityRef: value.GetBotIdentityRef(), BotUsername: value.GetBotUsername(),
		BotProviderRevision: value.GetBotProviderRevision(), BotProviderGeneration: value.GetBotProviderGeneration(),
		BotProviderTeamRef: value.GetBotProviderTeamRef(), BotMaskedStatus: value.GetBotMaskedStatus(),
		BotReceiptID: value.GetBotReceiptId(), BotReceiptVersion: value.GetBotReceiptVersion(),
		BotReceiptSHA256: value.GetBotReceiptSha256(), Enabled: value.GetEnabled(),
		Ownership: configurationOwnershipFromProto(value.GetOwnership()),
	}
}

func agentToProto(value entity.AgentSpec) *controlplanev1.AgentSpec {
	return &controlplanev1.AgentSpec{
		StableKey: value.StableKey, RoleDefinitionId: value.RoleDefinitionID,
		RoleDefinitionVersion: value.RoleDefinitionVersion, RoleDefinitionSha256: value.RoleDefinitionSHA256,
		InstructionSetId: value.InstructionSetID, InstructionSetVersion: value.InstructionSetVersion,
		InstructionSetSha256: value.InstructionSetSHA256, ProviderPoolId: value.ProviderPoolID,
		ProviderPoolVersion: value.ProviderPoolVersion, ProviderPoolSha256: value.ProviderPoolSHA256,
		RuntimeProfileRef: value.RuntimeProfileRef, RuntimeProfileVersion: value.RuntimeProfileVersion,
		RuntimeProfileSha256: value.RuntimeProfileSHA256, Capabilities: value.Capabilities,
		BotIdentityRef: value.BotIdentityRef, BotUsername: value.BotUsername,
		BotProviderRevision: value.BotProviderRevision, BotProviderGeneration: value.BotProviderGeneration,
		BotProviderTeamRef: value.BotProviderTeamRef, BotMaskedStatus: value.BotMaskedStatus,
		BotReceiptId: value.BotReceiptID, BotReceiptVersion: value.BotReceiptVersion,
		BotReceiptSha256: value.BotReceiptSHA256, Enabled: value.Enabled,
		Ownership: configurationOwnershipToProto(value.Ownership),
	}
}

func agentAssignmentFromProto(value *controlplanev1.AgentAssignmentSpec) entity.AgentAssignmentSpec {
	return entity.AgentAssignmentSpec{
		AgentID: value.GetAgentId(), AgentVersion: value.GetAgentVersion(), AgentSHA256: value.GetAgentSha256(),
		WorkspaceID: value.GetWorkspaceId(), WorkspaceVersion: value.GetWorkspaceVersion(),
		WorkspaceSHA256: value.GetWorkspaceSha256(), RoomID: value.GetRoomId(),
		RootActorID: value.GetRootActorId(), AssignmentGeneration: value.GetAssignmentGeneration(),
	}
}

func agentAssignmentToProto(value entity.AgentAssignmentSpec) *controlplanev1.AgentAssignmentSpec {
	return &controlplanev1.AgentAssignmentSpec{
		AgentId: value.AgentID, AgentVersion: value.AgentVersion, AgentSha256: value.AgentSHA256,
		WorkspaceId: value.WorkspaceID, WorkspaceVersion: value.WorkspaceVersion,
		WorkspaceSha256: value.WorkspaceSHA256, RoomId: value.RoomID,
		RootActorId: value.RootActorID, AssignmentGeneration: value.AssignmentGeneration,
	}
}

func instructionSetFromProto(value *controlplanev1.InstructionSetSpec) entity.InstructionSetSpec {
	return entity.InstructionSetSpec{
		StableKey: value.GetStableKey(), Locale: value.GetLocale(), CurrentVersion: value.GetCurrentVersion(),
		PublishedVersion: value.GetPublishedVersion(), Content: value.GetContent(), ContentSHA256: value.GetContentSha256(),
		VersionState:     trimEnum(value.GetVersionState().String(), "INSTRUCTION_VERSION_STATE_"),
		ValidationSHA256: value.GetValidationSha256(), RollbackOfVersion: value.GetRollbackOfVersion(),
		ValidationSucceeded: value.GetValidationSucceeded(), ValidatedContentVersion: value.GetValidatedContentVersion(),
		ValidatedContentSHA256: value.GetValidatedContentSha256(), ValidationErrors: validationErrorsFromProto(value.GetValidationErrors()),
		ContentArtifactID: value.GetContentArtifactId(), ContentArtifactVersion: value.GetContentArtifactVersion(),
		Ownership: configurationOwnershipFromProto(value.GetOwnership()),
	}
}

func instructionSetToProto(value entity.InstructionSetSpec) *controlplanev1.InstructionSetSpec {
	return &controlplanev1.InstructionSetSpec{
		StableKey: value.StableKey, Locale: value.Locale, CurrentVersion: value.CurrentVersion,
		PublishedVersion: value.PublishedVersion, Content: value.Content, ContentSha256: value.ContentSHA256,
		VersionState: instructionVersionStateToProto(value.VersionState), ValidationSha256: value.ValidationSHA256,
		RollbackOfVersion: value.RollbackOfVersion, Ownership: configurationOwnershipToProto(value.Ownership),
		ValidationSucceeded: value.ValidationSucceeded, ValidatedContentVersion: value.ValidatedContentVersion,
		ValidatedContentSha256: value.ValidatedContentSHA256, ValidationErrors: validationErrorsToProto(value.ValidationErrors),
		ContentArtifactId: value.ContentArtifactID, ContentArtifactVersion: value.ContentArtifactVersion,
	}
}

func providerReferenceFromProto(value *controlplanev1.ProviderConnectionReferenceSpec) (entity.Spec, error) {
	observedAt, err := requiredTime(value.GetObservedAt())
	if err != nil {
		return nil, err
	}
	return entity.ProviderConnectionReferenceSpec{
		StableKey: value.GetStableKey(), Provider: value.GetProvider(), ServerReference: value.GetServerReference(),
		ReferenceVersion: value.GetReferenceVersion(), ReferenceGeneration: value.GetReferenceGeneration(),
		ReferenceSHA256: value.GetReferenceSha256(),
		MaskedLabel:     value.GetMaskedLabel(), MaskedStatus: trimEnum(value.GetMaskedStatus().String(), "PROVIDER_CONNECTION_STATUS_"),
		Capabilities: value.GetCapabilities(), Eligible: value.GetEligible(), ObservedAt: observedAt,
		ReceiptID: value.GetReceiptId(), ReceiptVersion: value.GetReceiptVersion(), ReceiptSHA256: value.GetReceiptSha256(),
		CredentialBindingID: value.GetCredentialBindingId(), CredentialBindingVersion: value.GetCredentialBindingVersion(),
		CredentialBindingSHA256: value.GetCredentialBindingSha256(),
	}, nil
}

func providerReferenceToProto(value entity.ProviderConnectionReferenceSpec) *controlplanev1.ProviderConnectionReferenceSpec {
	return &controlplanev1.ProviderConnectionReferenceSpec{
		StableKey: value.StableKey, Provider: value.Provider, ServerReference: value.ServerReference,
		ReferenceVersion: value.ReferenceVersion, ReferenceGeneration: value.ReferenceGeneration,
		ReferenceSha256: value.ReferenceSHA256,
		MaskedLabel:     value.MaskedLabel, MaskedStatus: providerConnectionStatusToProto(value.MaskedStatus),
		Capabilities: value.Capabilities, Eligible: value.Eligible, ObservedAt: timestamppb.New(value.ObservedAt),
		ReceiptId: value.ReceiptID, ReceiptVersion: value.ReceiptVersion, ReceiptSha256: value.ReceiptSHA256,
		CredentialBindingId: value.CredentialBindingID, CredentialBindingVersion: value.CredentialBindingVersion,
		CredentialBindingSha256: value.CredentialBindingSHA256,
	}
}

func validationErrorsFromProto(values []*controlplanev1.InstructionValidationError) []entity.InstructionValidationError {
	result := make([]entity.InstructionValidationError, 0, len(values))
	for _, item := range values {
		result = append(result, entity.InstructionValidationError{Code: item.GetCode(), Field: item.GetField(),
			Line: item.GetLine(), Column: item.GetColumn(), Message: item.GetMessage()})
	}
	return result
}

func validationErrorsToProto(values []entity.InstructionValidationError) []*controlplanev1.InstructionValidationError {
	result := make([]*controlplanev1.InstructionValidationError, 0, len(values))
	for _, item := range values {
		result = append(result, &controlplanev1.InstructionValidationError{Code: item.Code, Field: item.Field,
			Line: item.Line, Column: item.Column, Message: item.Message})
	}
	return result
}

func providerPoolFromProto(value *controlplanev1.ProviderPoolSpec) (entity.Spec, error) {
	observationMaxAge, err := optionalDuration(value.GetObservationMaxAge())
	if err != nil {
		return nil, err
	}
	bindings := make([]entity.ProviderPoolBinding, 0, len(value.GetBindings()))
	for _, binding := range value.GetBindings() {
		bindings = append(bindings, entity.ProviderPoolBinding{
			ProviderConnectionReferenceID: binding.GetProviderConnectionReferenceId(),
			ProviderConnectionStableKey:   binding.GetProviderConnectionStableKey(),
			ReferenceVersion:              binding.GetReferenceVersion(), ReferenceSHA256: binding.GetReferenceSha256(),
			Weight: binding.GetWeight(), Eligible: binding.GetEligible(),
			MaskedStatus: trimEnum(binding.GetMaskedStatus().String(), "PROVIDER_CONNECTION_STATUS_"),
		})
	}
	return entity.ProviderPoolSpec{
		StableKey: value.GetStableKey(), Policy: value.GetPolicy(), PolicyRevision: value.GetPolicyRevision(),
		ObservationMaxAge: observationMaxAge, Bindings: bindings,
		EligibilitySnapshotSHA256: value.GetEligibilitySnapshotSha256(),
		Ownership:                 configurationOwnershipFromProto(value.GetOwnership()),
	}, nil
}

func providerPoolToProto(value entity.ProviderPoolSpec) *controlplanev1.ProviderPoolSpec {
	bindings := make([]*controlplanev1.ProviderPoolBinding, 0, len(value.Bindings))
	for _, binding := range value.Bindings {
		bindings = append(bindings, &controlplanev1.ProviderPoolBinding{
			ProviderConnectionReferenceId: binding.ProviderConnectionReferenceID,
			ProviderConnectionStableKey:   binding.ProviderConnectionStableKey,
			ReferenceVersion:              binding.ReferenceVersion, ReferenceSha256: binding.ReferenceSHA256,
			Weight: binding.Weight, Eligible: binding.Eligible,
			MaskedStatus: providerConnectionStatusToProto(binding.MaskedStatus),
		})
	}
	return &controlplanev1.ProviderPoolSpec{
		StableKey: value.StableKey, Policy: value.Policy, PolicyRevision: value.PolicyRevision,
		ObservationMaxAge: durationpb.New(value.ObservationMaxAge), Bindings: bindings,
		EligibilitySnapshotSha256: value.EligibilitySnapshotSHA256,
		Ownership:                 configurationOwnershipToProto(value.Ownership),
	}
}

func workspaceBackupFromProto(value *controlplanev1.WorkspaceBackupSpec) (entity.Spec, error) {
	retainUntil, err := requiredTime(value.GetRetainUntil())
	if err != nil {
		return nil, err
	}
	members := make([]entity.WorkspaceBackupMember, 0, len(value.GetMembers()))
	for _, member := range value.GetMembers() {
		members = append(members, entity.WorkspaceBackupMember{
			SourceExecutionID: member.GetSourceExecutionId(),
			WorkspaceID:       member.GetWorkspaceId(), WorkspaceVersion: member.GetWorkspaceVersion(),
			WorkspaceSHA256: member.GetWorkspaceSha256(), SessionID: member.GetSessionId(),
			SourceVersion: member.GetSourceVersion(), RuntimeRevisionSHA256: member.GetRuntimeRevisionSha256(),
			ImmutableInputSHA256: member.GetImmutableInputSha256(), ArchiveSHA256: member.GetArchiveSha256(),
			ProvenanceSHA256: member.GetProvenanceSha256(),
		})
	}
	return entity.WorkspaceBackupSpec{
		Scope: trimEnum(value.GetScope().String(), "WORKSPACE_BACKUP_SCOPE_"), ScopeID: value.GetScopeId(),
		Members: members, MembershipSHA256: value.GetMembershipSha256(),
		BackupState: trimEnum(value.GetBackupState().String(), "WORKSPACE_BACKUP_STATE_"),
		Attempt:     value.GetAttempt(), Generation: value.GetGeneration(), RevokedGeneration: value.GetRevokedGeneration(),
		TerminalReasonCode: value.GetTerminalReasonCode(), RetainUntil: retainUntil,
	}, nil
}

func workspaceBackupToProto(value entity.WorkspaceBackupSpec) *controlplanev1.WorkspaceBackupSpec {
	members := make([]*controlplanev1.WorkspaceBackupMember, 0, len(value.Members))
	for _, member := range value.Members {
		members = append(members, workspaceBackupMemberToProto(member))
	}
	return &controlplanev1.WorkspaceBackupSpec{
		Scope: workspaceBackupScopeToProto(value.Scope), ScopeId: value.ScopeID, Members: members,
		MembershipSha256: value.MembershipSHA256, BackupState: workspaceBackupStateToProto(value.BackupState),
		Attempt: value.Attempt, Generation: value.Generation, RevokedGeneration: value.RevokedGeneration,
		TerminalReasonCode: value.TerminalReasonCode, RetainUntil: timestamppb.New(value.RetainUntil),
	}
}

func workspaceRestoreFromProto(value *controlplanev1.WorkspaceRestoreSpec) entity.WorkspaceRestoreSpec {
	members := make([]entity.WorkspaceRestoreMember, 0, len(value.GetMembers()))
	for _, member := range value.GetMembers() {
		members = append(members, entity.WorkspaceRestoreMember{
			SourceExecutionID: member.GetSourceExecutionId(),
			WorkspaceID:       member.GetWorkspaceId(),
			SourceSessionID:   member.GetSourceSessionId(), TargetTurnID: member.GetTargetTurnId(),
			TargetTurnVersion: member.GetTargetTurnVersion(), TargetAttempt: member.GetTargetAttempt(),
			RuntimeRevisionID: member.GetRuntimeRevisionId(), RuntimeRevisionVersion: member.GetRuntimeRevisionVersion(),
			ImmutableInputSHA256: member.GetImmutableInputSha256(), GrantSHA256: member.GetGrantSha256(),
			State: trimEnum(member.GetState().String(), "WORKSPACE_RESTORE_STATE_"),
		})
	}
	return entity.WorkspaceRestoreSpec{
		BackupID: value.GetBackupId(), BackupVersion: value.GetBackupVersion(),
		MembershipSHA256: value.GetMembershipSha256(), Members: members,
		RestoreState: trimEnum(value.GetRestoreState().String(), "WORKSPACE_RESTORE_STATE_"),
		Attempt:      value.GetAttempt(), Generation: value.GetGeneration(), RevokedGeneration: value.GetRevokedGeneration(),
		Partial: value.GetPartial(), TerminalReasonCode: value.GetTerminalReasonCode(),
	}
}

func workspaceRestoreToProto(value entity.WorkspaceRestoreSpec) *controlplanev1.WorkspaceRestoreSpec {
	members := make([]*controlplanev1.WorkspaceRestoreMember, 0, len(value.Members))
	for _, member := range value.Members {
		members = append(members, workspaceRestoreMemberToProto(member))
	}
	return &controlplanev1.WorkspaceRestoreSpec{
		BackupId: value.BackupID, BackupVersion: value.BackupVersion, MembershipSha256: value.MembershipSHA256,
		Members: members, RestoreState: workspaceRestoreStateToProto(value.RestoreState),
		Attempt: value.Attempt, Generation: value.Generation, RevokedGeneration: value.RevokedGeneration,
		Partial: value.Partial, TerminalReasonCode: value.TerminalReasonCode,
	}
}

func workspaceMappingFromProto(value *controlplanev1.WorkspaceMattermostMappingSpec) (entity.Spec, error) {
	observedAt, err := requiredTime(value.GetProviderObservedAt())
	if err != nil {
		return nil, err
	}
	return entity.WorkspaceMattermostMappingSpec{
		WorkspaceID: value.GetWorkspaceId(), WorkspaceVersion: value.GetWorkspaceVersion(),
		WorkspaceSHA256: value.GetWorkspaceSha256(), ProviderTeamRef: value.GetProviderTeamRef(),
		ProviderReceiptID: value.GetProviderReceiptId(), ProviderReceiptVersion: value.GetProviderReceiptVersion(),
		ProviderReceiptSHA256: value.GetProviderReceiptSha256(), ProviderEffectVersion: value.GetProviderEffectVersion(),
		ProviderEffectGeneration: value.GetProviderEffectGeneration(), MappingGeneration: value.GetMappingGeneration(),
		MappingState:       trimEnum(value.GetMappingState().String(), "WORKSPACE_MATTERMOST_MAPPING_STATE_"),
		ProviderObservedAt: observedAt,
	}, nil
}

func workspaceMappingToProto(value entity.WorkspaceMattermostMappingSpec) *controlplanev1.WorkspaceMattermostMappingSpec {
	return &controlplanev1.WorkspaceMattermostMappingSpec{
		WorkspaceId: value.WorkspaceID, WorkspaceVersion: value.WorkspaceVersion,
		WorkspaceSha256: value.WorkspaceSHA256, ProviderTeamRef: value.ProviderTeamRef,
		ProviderReceiptId: value.ProviderReceiptID, ProviderReceiptVersion: value.ProviderReceiptVersion,
		ProviderReceiptSha256: value.ProviderReceiptSHA256, ProviderEffectVersion: value.ProviderEffectVersion,
		ProviderEffectGeneration: value.ProviderEffectGeneration, MappingGeneration: value.MappingGeneration,
		MappingState:       workspaceMappingStateToProto(value.MappingState),
		ProviderObservedAt: timestamppb.New(value.ProviderObservedAt),
	}
}

func workspaceBackupMemberToProto(value entity.WorkspaceBackupMember) *controlplanev1.WorkspaceBackupMember {
	return &controlplanev1.WorkspaceBackupMember{
		SourceExecutionId: value.SourceExecutionID,
		WorkspaceId:       value.WorkspaceID, WorkspaceVersion: value.WorkspaceVersion, WorkspaceSha256: value.WorkspaceSHA256,
		SessionId: value.SessionID, SourceVersion: value.SourceVersion, RuntimeRevisionSha256: value.RuntimeRevisionSHA256,
		ImmutableInputSha256: value.ImmutableInputSHA256, ArchiveSha256: value.ArchiveSHA256,
		ProvenanceSha256: value.ProvenanceSHA256,
	}
}

func workspaceRestoreMemberToProto(value entity.WorkspaceRestoreMember) *controlplanev1.WorkspaceRestoreMember {
	return &controlplanev1.WorkspaceRestoreMember{
		SourceExecutionId: value.SourceExecutionID,
		WorkspaceId:       value.WorkspaceID,
		SourceSessionId:   value.SourceSessionID, TargetTurnId: value.TargetTurnID,
		TargetTurnVersion: value.TargetTurnVersion, TargetAttempt: value.TargetAttempt,
		RuntimeRevisionId: value.RuntimeRevisionID, RuntimeRevisionVersion: value.RuntimeRevisionVersion,
		ImmutableInputSha256: value.ImmutableInputSHA256, GrantSha256: value.GrantSHA256,
		State: workspaceRestoreStateToProto(value.State),
	}
}

func instructionVersionStateToProto(value string) controlplanev1.InstructionVersionState {
	return map[string]controlplanev1.InstructionVersionState{
		"DRAFT":     controlplanev1.InstructionVersionState_INSTRUCTION_VERSION_STATE_DRAFT,
		"VALIDATED": controlplanev1.InstructionVersionState_INSTRUCTION_VERSION_STATE_VALIDATED,
		"PUBLISHED": controlplanev1.InstructionVersionState_INSTRUCTION_VERSION_STATE_PUBLISHED,
		"REJECTED":  controlplanev1.InstructionVersionState_INSTRUCTION_VERSION_STATE_REJECTED,
		"ARCHIVED":  controlplanev1.InstructionVersionState_INSTRUCTION_VERSION_STATE_ARCHIVED,
	}[value]
}

func providerConnectionStatusToProto(value string) controlplanev1.ProviderConnectionStatus {
	return map[string]controlplanev1.ProviderConnectionStatus{
		"AVAILABLE":  controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_AVAILABLE,
		"DEGRADED":   controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_DEGRADED,
		"INELIGIBLE": controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_INELIGIBLE,
		"ARCHIVED":   controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_ARCHIVED,
	}[value]
}

func workspaceBackupScopeToProto(value string) controlplanev1.WorkspaceBackupScope {
	return map[string]controlplanev1.WorkspaceBackupScope{
		"WORKSPACE":      controlplanev1.WorkspaceBackupScope_WORKSPACE_BACKUP_SCOPE_WORKSPACE,
		"ALL_WORKSPACES": controlplanev1.WorkspaceBackupScope_WORKSPACE_BACKUP_SCOPE_ALL_WORKSPACES,
	}[value]
}

func workspaceBackupStateToProto(value string) controlplanev1.WorkspaceBackupState {
	return map[string]controlplanev1.WorkspaceBackupState{
		"VERIFYING": controlplanev1.WorkspaceBackupState_WORKSPACE_BACKUP_STATE_VERIFYING,
		"AVAILABLE": controlplanev1.WorkspaceBackupState_WORKSPACE_BACKUP_STATE_AVAILABLE,
		"FAILED":    controlplanev1.WorkspaceBackupState_WORKSPACE_BACKUP_STATE_FAILED,
		"CANCELLED": controlplanev1.WorkspaceBackupState_WORKSPACE_BACKUP_STATE_CANCELLED,
		"EXPIRED":   controlplanev1.WorkspaceBackupState_WORKSPACE_BACKUP_STATE_EXPIRED,
	}[value]
}

func workspaceRestoreStateToProto(value string) controlplanev1.WorkspaceRestoreState {
	return map[string]controlplanev1.WorkspaceRestoreState{
		"QUEUED":    controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_QUEUED,
		"RUNNING":   controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_RUNNING,
		"SUCCEEDED": controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_SUCCEEDED,
		"FAILED":    controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_FAILED,
		"CANCELLED": controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_CANCELLED,
		"EXPIRED":   controlplanev1.WorkspaceRestoreState_WORKSPACE_RESTORE_STATE_EXPIRED,
	}[value]
}

func workspaceMappingStateToProto(value string) controlplanev1.WorkspaceMattermostMappingState {
	return map[string]controlplanev1.WorkspaceMattermostMappingState{
		"BOUND":    controlplanev1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_BOUND,
		"UNLINKED": controlplanev1.WorkspaceMattermostMappingState_WORKSPACE_MATTERMOST_MAPPING_STATE_UNLINKED,
	}[value]
}
