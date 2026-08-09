package enum

type AgentBotIdentityStatus string

const (
	AgentBotIdentityAvailable AgentBotIdentityStatus = "AVAILABLE"
	AgentBotIdentityRevoked   AgentBotIdentityStatus = "REVOKED"
	AgentBotIdentityDeleted   AgentBotIdentityStatus = "DELETED"
	AgentBotIdentityUnknown   AgentBotIdentityStatus = "UNKNOWN"
)

type AgentBotIdentityOperationState string

const (
	AgentBotOperationEffectPending     AgentBotIdentityOperationState = "EFFECT_PENDING"
	AgentBotOperationMembershipPending AgentBotIdentityOperationState = "MEMBERSHIP_PENDING"
	AgentBotOperationAmbiguous         AgentBotIdentityOperationState = "AMBIGUOUS"
	AgentBotOperationProviderAccepted  AgentBotIdentityOperationState = "PROVIDER_ACCEPTED"
	AgentBotOperationBound             AgentBotIdentityOperationState = "BOUND"
	AgentBotOperationRevoked           AgentBotIdentityOperationState = "REVOKED"
	AgentBotOperationRepairRequired    AgentBotIdentityOperationState = "REPAIR_REQUIRED"
)

const (
	AgentBotActionCreateAndBind = "create_and_bind"
	AgentBotActionBind          = "bind"
	AgentBotActionRebind        = "rebind"
	AgentBotActionRevoke        = "revoke"
)
