package enum

type ScopeType string

const (
	ScopeTypePlatform     ScopeType = "platform"
	ScopeTypeOrganization ScopeType = "organization"
	ScopeTypeProject      ScopeType = "project"
	ScopeTypeRepository   ScopeType = "repository"
	ScopeTypeService      ScopeType = "service"
)

func (v ScopeType) Valid() bool {
	switch v {
	case ScopeTypePlatform, ScopeTypeOrganization, ScopeTypeProject, ScopeTypeRepository, ScopeTypeService:
		return true
	default:
		return false
	}
}

type ConversationThreadKind string

const (
	ConversationThreadKindUserDialog    ConversationThreadKind = "user_dialog"
	ConversationThreadKindOwnerFeedback ConversationThreadKind = "owner_feedback"
	ConversationThreadKindApproval      ConversationThreadKind = "approval"
	ConversationThreadKindHumanGate     ConversationThreadKind = "human_gate"
	ConversationThreadKindNotification  ConversationThreadKind = "notification"
	ConversationThreadKindOps           ConversationThreadKind = "ops"
)

func (v ConversationThreadKind) Valid() bool {
	switch v {
	case ConversationThreadKindUserDialog, ConversationThreadKindOwnerFeedback, ConversationThreadKindApproval, ConversationThreadKindHumanGate, ConversationThreadKindNotification, ConversationThreadKindOps:
		return true
	default:
		return false
	}
}

type ConversationThreadStatus string

const (
	ConversationThreadStatusOpen     ConversationThreadStatus = "open"
	ConversationThreadStatusWaiting  ConversationThreadStatus = "waiting"
	ConversationThreadStatusClosed   ConversationThreadStatus = "closed"
	ConversationThreadStatusArchived ConversationThreadStatus = "archived"
)

type ConversationSourceKind string

const (
	ConversationSourceKindWebConsole     ConversationSourceKind = "web_console"
	ConversationSourceKindVoice          ConversationSourceKind = "voice"
	ConversationSourceKindMCP            ConversationSourceKind = "mcp"
	ConversationSourceKindProvider       ConversationSourceKind = "provider"
	ConversationSourceKindChannelPackage ConversationSourceKind = "channel_package"
	ConversationSourceKindCodexHook      ConversationSourceKind = "codex_hook"
	ConversationSourceKindSystem         ConversationSourceKind = "system"
	ConversationSourceKindService        ConversationSourceKind = "service"
)

func (v ConversationSourceKind) Valid() bool {
	switch v {
	case ConversationSourceKindWebConsole, ConversationSourceKindVoice, ConversationSourceKindMCP, ConversationSourceKindProvider, ConversationSourceKindChannelPackage, ConversationSourceKindCodexHook, ConversationSourceKindSystem, ConversationSourceKindService:
		return true
	default:
		return false
	}
}

type ConversationMessageKind string

const (
	ConversationMessageKindUserText        ConversationMessageKind = "user_text"
	ConversationMessageKindVoiceTranscript ConversationMessageKind = "voice_transcript"
	ConversationMessageKindAgentText       ConversationMessageKind = "agent_text"
	ConversationMessageKindSystemNotice    ConversationMessageKind = "system_notice"
	ConversationMessageKindResponseSummary ConversationMessageKind = "response_summary"
	ConversationMessageKindCallbackSummary ConversationMessageKind = "callback_summary"
)

func (v ConversationMessageKind) Valid() bool {
	switch v {
	case ConversationMessageKindUserText, ConversationMessageKindVoiceTranscript, ConversationMessageKindAgentText, ConversationMessageKindSystemNotice, ConversationMessageKindResponseSummary, ConversationMessageKindCallbackSummary:
		return true
	default:
		return false
	}
}

type SourceOwnerKind string

const (
	SourceOwnerKindAgentManager      SourceOwnerKind = "agent_manager"
	SourceOwnerKindSlotAgent         SourceOwnerKind = "slot_agent"
	SourceOwnerKindGovernanceManager SourceOwnerKind = "governance_manager"
	SourceOwnerKindProviderHub       SourceOwnerKind = "provider_hub"
	SourceOwnerKindOperationsHub     SourceOwnerKind = "operations_hub"
	SourceOwnerKindUser              SourceOwnerKind = "user"
	SourceOwnerKindSystem            SourceOwnerKind = "system"
)

func (v SourceOwnerKind) Valid() bool {
	switch v {
	case SourceOwnerKindAgentManager, SourceOwnerKindSlotAgent, SourceOwnerKindGovernanceManager, SourceOwnerKindProviderHub, SourceOwnerKindOperationsHub, SourceOwnerKindUser, SourceOwnerKindSystem:
		return true
	default:
		return false
	}
}

type DecisionOwnerKind string

const (
	DecisionOwnerKindAgentManager      DecisionOwnerKind = "agent_manager"
	DecisionOwnerKindGovernanceManager DecisionOwnerKind = "governance_manager"
	DecisionOwnerKindProviderHub       DecisionOwnerKind = "provider_hub"
	DecisionOwnerKindOperationsHub     DecisionOwnerKind = "operations_hub"
	DecisionOwnerKindSystem            DecisionOwnerKind = "system"
)

func (v DecisionOwnerKind) Valid() bool {
	switch v {
	case DecisionOwnerKindAgentManager, DecisionOwnerKindGovernanceManager, DecisionOwnerKindProviderHub, DecisionOwnerKindOperationsHub, DecisionOwnerKindSystem:
		return true
	default:
		return false
	}
}

type IngressKind string

const (
	IngressKindDirectGRPC IngressKind = "direct_grpc"
	IngressKindMCP        IngressKind = "mcp"
	IngressKindCodexHook  IngressKind = "codex_hook"
	IngressKindGateway    IngressKind = "gateway"
	IngressKindSystem     IngressKind = "system"
	IngressKindService    IngressKind = "service"
)

func (v IngressKind) Valid() bool {
	switch v {
	case IngressKindDirectGRPC, IngressKindMCP, IngressKindCodexHook, IngressKindGateway, IngressKindSystem, IngressKindService:
		return true
	default:
		return false
	}
}

type InteractionRequestKind string

const (
	InteractionRequestKindFeedback  InteractionRequestKind = "feedback"
	InteractionRequestKindApproval  InteractionRequestKind = "approval"
	InteractionRequestKindHumanGate InteractionRequestKind = "human_gate"
)

func (v InteractionRequestKind) Valid() bool {
	switch v {
	case InteractionRequestKindFeedback, InteractionRequestKindApproval, InteractionRequestKindHumanGate:
		return true
	default:
		return false
	}
}

type InteractionRiskClass string

const (
	InteractionRiskClassLow      InteractionRiskClass = "low"
	InteractionRiskClassMedium   InteractionRiskClass = "medium"
	InteractionRiskClassHigh     InteractionRiskClass = "high"
	InteractionRiskClassCritical InteractionRiskClass = "critical"
)

func (v InteractionRiskClass) Valid() bool {
	switch v {
	case InteractionRiskClassLow, InteractionRiskClassMedium, InteractionRiskClassHigh, InteractionRiskClassCritical:
		return true
	default:
		return false
	}
}

type InteractionRequestStatus string

const (
	InteractionRequestStatusCreated   InteractionRequestStatus = "created"
	InteractionRequestStatusRouted    InteractionRequestStatus = "routed"
	InteractionRequestStatusWaiting   InteractionRequestStatus = "waiting"
	InteractionRequestStatusAnswered  InteractionRequestStatus = "answered"
	InteractionRequestStatusExpired   InteractionRequestStatus = "expired"
	InteractionRequestStatusCancelled InteractionRequestStatus = "cancelled"
	InteractionRequestStatusFailed    InteractionRequestStatus = "failed"
)

func (v InteractionRequestStatus) Valid() bool {
	switch v {
	case InteractionRequestStatusCreated, InteractionRequestStatusRouted, InteractionRequestStatusWaiting, InteractionRequestStatusAnswered, InteractionRequestStatusExpired, InteractionRequestStatusCancelled, InteractionRequestStatusFailed:
		return true
	default:
		return false
	}
}

func (v InteractionRequestStatus) Terminal() bool {
	switch v {
	case InteractionRequestStatusAnswered, InteractionRequestStatusExpired, InteractionRequestStatusCancelled, InteractionRequestStatusFailed:
		return true
	default:
		return false
	}
}

type InteractionResponseAction string

const (
	InteractionResponseActionAnswer      InteractionResponseAction = "answer"
	InteractionResponseActionApprove     InteractionResponseAction = "approve"
	InteractionResponseActionReject      InteractionResponseAction = "reject"
	InteractionResponseActionDefer       InteractionResponseAction = "defer"
	InteractionResponseActionAcknowledge InteractionResponseAction = "acknowledge"
	InteractionResponseActionCustom      InteractionResponseAction = "custom"
)

func (v InteractionResponseAction) Valid() bool {
	switch v {
	case InteractionResponseActionAnswer, InteractionResponseActionApprove, InteractionResponseActionReject, InteractionResponseActionDefer, InteractionResponseActionAcknowledge, InteractionResponseActionCustom:
		return true
	default:
		return false
	}
}

type InteractionResponseSourceKind string

const (
	InteractionResponseSourceKindWebConsole      InteractionResponseSourceKind = "web_console"
	InteractionResponseSourceKindMCP             InteractionResponseSourceKind = "mcp"
	InteractionResponseSourceKindChannelCallback InteractionResponseSourceKind = "channel_callback"
	InteractionResponseSourceKindSystem          InteractionResponseSourceKind = "system"
	InteractionResponseSourceKindService         InteractionResponseSourceKind = "service"
)

func (v InteractionResponseSourceKind) Valid() bool {
	switch v {
	case InteractionResponseSourceKindWebConsole, InteractionResponseSourceKindMCP, InteractionResponseSourceKindChannelCallback, InteractionResponseSourceKindSystem, InteractionResponseSourceKindService:
		return true
	default:
		return false
	}
}
