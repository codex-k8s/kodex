package enum

type MattermostTeamStatus string

const (
	MattermostTeamActive  MattermostTeamStatus = "ACTIVE"
	MattermostTeamDeleted MattermostTeamStatus = "DELETED"
)

type MattermostTeamOperationState string

const (
	TeamOperationPending          MattermostTeamOperationState = "PENDING"
	TeamOperationEffectPending    MattermostTeamOperationState = "EFFECT_PENDING"
	TeamOperationAmbiguous        MattermostTeamOperationState = "AMBIGUOUS"
	TeamOperationProviderAccepted MattermostTeamOperationState = "PROVIDER_ACCEPTED"
	TeamOperationRepairRequired   MattermostTeamOperationState = "REPAIR_REQUIRED"
)
