package enum

type MattermostTeamStatus string

const (
	MattermostTeamActive  MattermostTeamStatus = "ACTIVE"
	MattermostTeamDeleted MattermostTeamStatus = "DELETED"
)

const (
	WorkspaceMappingOperationPending        = "PENDING"
	WorkspaceMappingOperationAmbiguous      = "AMBIGUOUS"
	WorkspaceMappingOperationBound          = "BOUND"
	WorkspaceMappingOperationUnlinked       = "UNLINKED"
	WorkspaceMappingOperationRepairRequired = "REPAIR_REQUIRED"
)

type MattermostTeamOperationState string

const (
	TeamOperationPending          MattermostTeamOperationState = "PENDING"
	TeamOperationEffectPending    MattermostTeamOperationState = "EFFECT_PENDING"
	TeamOperationAmbiguous        MattermostTeamOperationState = "AMBIGUOUS"
	TeamOperationProviderAccepted MattermostTeamOperationState = "PROVIDER_ACCEPTED"
	TeamOperationRepairRequired   MattermostTeamOperationState = "REPAIR_REQUIRED"
)
