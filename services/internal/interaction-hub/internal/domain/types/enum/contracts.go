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
	InteractionResponseActionAnswer         InteractionResponseAction = "answer"
	InteractionResponseActionApprove        InteractionResponseAction = "approve"
	InteractionResponseActionReject         InteractionResponseAction = "reject"
	InteractionResponseActionRequestChanges InteractionResponseAction = "request_changes"
	InteractionResponseActionDefer          InteractionResponseAction = "defer"
	InteractionResponseActionAcknowledge    InteractionResponseAction = "acknowledge"
	InteractionResponseActionCustom         InteractionResponseAction = "custom"
)

func (v InteractionResponseAction) Valid() bool {
	switch v {
	case InteractionResponseActionAnswer, InteractionResponseActionApprove, InteractionResponseActionReject, InteractionResponseActionRequestChanges, InteractionResponseActionDefer, InteractionResponseActionAcknowledge, InteractionResponseActionCustom:
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

type NotificationKind string

const (
	NotificationKindStatus           NotificationKind = "status"
	NotificationKindReminder         NotificationKind = "reminder"
	NotificationKindError            NotificationKind = "error"
	NotificationKindAttention        NotificationKind = "attention"
	NotificationKindDecisionRequired NotificationKind = "decision_required"
	NotificationKindOps              NotificationKind = "ops"
)

func (v NotificationKind) Valid() bool {
	switch v {
	case NotificationKindStatus, NotificationKindReminder, NotificationKindError, NotificationKindAttention, NotificationKindDecisionRequired, NotificationKindOps:
		return true
	default:
		return false
	}
}

type NotificationPriority string

const (
	NotificationPriorityLow    NotificationPriority = "low"
	NotificationPriorityNormal NotificationPriority = "normal"
	NotificationPriorityHigh   NotificationPriority = "high"
	NotificationPriorityUrgent NotificationPriority = "urgent"
)

func (v NotificationPriority) Valid() bool {
	switch v {
	case NotificationPriorityLow, NotificationPriorityNormal, NotificationPriorityHigh, NotificationPriorityUrgent:
		return true
	default:
		return false
	}
}

type NotificationStatus string

const (
	NotificationStatusCreated      NotificationStatus = "created"
	NotificationStatusQueued       NotificationStatus = "queued"
	NotificationStatusDelivered    NotificationStatus = "delivered"
	NotificationStatusAcknowledged NotificationStatus = "acknowledged"
	NotificationStatusExpired      NotificationStatus = "expired"
	NotificationStatusFailed       NotificationStatus = "failed"
)

func (v NotificationStatus) Valid() bool {
	switch v {
	case NotificationStatusCreated, NotificationStatusQueued, NotificationStatusDelivered, NotificationStatusAcknowledged, NotificationStatusExpired, NotificationStatusFailed:
		return true
	default:
		return false
	}
}

type SubscriptionStatus string

const (
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusPaused   SubscriptionStatus = "paused"
	SubscriptionStatusDisabled SubscriptionStatus = "disabled"
)

func (v SubscriptionStatus) Valid() bool {
	switch v {
	case SubscriptionStatusActive, SubscriptionStatusPaused, SubscriptionStatusDisabled:
		return true
	default:
		return false
	}
}

type DeliverySurfaceKind string

const (
	DeliverySurfaceKindWebConsole      DeliverySurfaceKind = "web_console"
	DeliverySurfaceKindVoice           DeliverySurfaceKind = "voice"
	DeliverySurfaceKindProviderSurface DeliverySurfaceKind = "provider_surface"
	DeliverySurfaceKindChannelPackage  DeliverySurfaceKind = "channel_package"
	DeliverySurfaceKindSystem          DeliverySurfaceKind = "system"
)

func (v DeliverySurfaceKind) Valid() bool {
	switch v {
	case DeliverySurfaceKindWebConsole, DeliverySurfaceKindVoice, DeliverySurfaceKindProviderSurface, DeliverySurfaceKindChannelPackage, DeliverySurfaceKindSystem:
		return true
	default:
		return false
	}
}

type DeliveryRouteStatus string

const (
	DeliveryRouteStatusActive   DeliveryRouteStatus = "active"
	DeliveryRouteStatusPaused   DeliveryRouteStatus = "paused"
	DeliveryRouteStatusDisabled DeliveryRouteStatus = "disabled"
)

func (v DeliveryRouteStatus) Valid() bool {
	switch v {
	case DeliveryRouteStatusActive, DeliveryRouteStatusPaused, DeliveryRouteStatusDisabled:
		return true
	default:
		return false
	}
}

type DeliveryKind string

const (
	DeliveryKindFeedback     DeliveryKind = "feedback"
	DeliveryKindApproval     DeliveryKind = "approval"
	DeliveryKindHumanGate    DeliveryKind = "human_gate"
	DeliveryKindNotification DeliveryKind = "notification"
)

func (v DeliveryKind) Valid() bool {
	switch v {
	case DeliveryKindFeedback, DeliveryKindApproval, DeliveryKindHumanGate, DeliveryKindNotification:
		return true
	default:
		return false
	}
}

type DeliveryAttemptStatus string

const (
	DeliveryAttemptStatusQueued    DeliveryAttemptStatus = "queued"
	DeliveryAttemptStatusSent      DeliveryAttemptStatus = "sent"
	DeliveryAttemptStatusAccepted  DeliveryAttemptStatus = "accepted"
	DeliveryAttemptStatusDelivered DeliveryAttemptStatus = "delivered"
	DeliveryAttemptStatusFailed    DeliveryAttemptStatus = "failed"
	DeliveryAttemptStatusCancelled DeliveryAttemptStatus = "cancelled"
	DeliveryAttemptStatusExpired   DeliveryAttemptStatus = "expired"
)

func (v DeliveryAttemptStatus) Valid() bool {
	switch v {
	case DeliveryAttemptStatusQueued, DeliveryAttemptStatusSent, DeliveryAttemptStatusAccepted, DeliveryAttemptStatusDelivered, DeliveryAttemptStatusFailed, DeliveryAttemptStatusCancelled, DeliveryAttemptStatusExpired:
		return true
	default:
		return false
	}
}

func (v DeliveryAttemptStatus) Terminal() bool {
	switch v {
	case DeliveryAttemptStatusDelivered, DeliveryAttemptStatusFailed, DeliveryAttemptStatusCancelled, DeliveryAttemptStatusExpired:
		return true
	default:
		return false
	}
}

type DeliveryErrorClass string

const (
	DeliveryErrorClassTemporary   DeliveryErrorClass = "temporary"
	DeliveryErrorClassPermanent   DeliveryErrorClass = "permanent"
	DeliveryErrorClassAuth        DeliveryErrorClass = "auth"
	DeliveryErrorClassRateLimited DeliveryErrorClass = "rate_limited"
	DeliveryErrorClassPolicy      DeliveryErrorClass = "policy"
)

func (v DeliveryErrorClass) Valid() bool {
	switch v {
	case DeliveryErrorClassTemporary, DeliveryErrorClassPermanent, DeliveryErrorClassAuth, DeliveryErrorClassRateLimited, DeliveryErrorClassPolicy:
		return true
	default:
		return false
	}
}

type ChannelDeliveryResultStatus string

const (
	ChannelDeliveryResultStatusAccepted  ChannelDeliveryResultStatus = "accepted"
	ChannelDeliveryResultStatusDeferred  ChannelDeliveryResultStatus = "deferred"
	ChannelDeliveryResultStatusRejected  ChannelDeliveryResultStatus = "rejected"
	ChannelDeliveryResultStatusFailed    ChannelDeliveryResultStatus = "failed"
	ChannelDeliveryResultStatusDelivered ChannelDeliveryResultStatus = "delivered"
	ChannelDeliveryResultStatusExpired   ChannelDeliveryResultStatus = "expired"
)

func (v ChannelDeliveryResultStatus) Valid() bool {
	switch v {
	case ChannelDeliveryResultStatusAccepted, ChannelDeliveryResultStatusDeferred, ChannelDeliveryResultStatusRejected, ChannelDeliveryResultStatusFailed, ChannelDeliveryResultStatusDelivered, ChannelDeliveryResultStatusExpired:
		return true
	default:
		return false
	}
}

type CallbackSignatureStatus string

const (
	CallbackSignatureStatusVerified             CallbackSignatureStatus = "verified"
	CallbackSignatureStatusTrustedInternal      CallbackSignatureStatus = "trusted_internal"
	CallbackSignatureStatusRejectedBeforeDomain CallbackSignatureStatus = "rejected_before_domain"
)

func (v CallbackSignatureStatus) Valid() bool {
	switch v {
	case CallbackSignatureStatusVerified, CallbackSignatureStatusTrustedInternal, CallbackSignatureStatusRejectedBeforeDomain:
		return true
	default:
		return false
	}
}

func (v CallbackSignatureStatus) Accepted() bool {
	switch v {
	case CallbackSignatureStatusVerified, CallbackSignatureStatusTrustedInternal:
		return true
	default:
		return false
	}
}

type CallbackProcessingStatus string

const (
	CallbackProcessingStatusAccepted  CallbackProcessingStatus = "accepted"
	CallbackProcessingStatusDuplicate CallbackProcessingStatus = "duplicate"
	CallbackProcessingStatusRejected  CallbackProcessingStatus = "rejected"
	CallbackProcessingStatusFailed    CallbackProcessingStatus = "failed"
)

func (v CallbackProcessingStatus) Valid() bool {
	switch v {
	case CallbackProcessingStatusAccepted, CallbackProcessingStatusDuplicate, CallbackProcessingStatusRejected, CallbackProcessingStatusFailed:
		return true
	default:
		return false
	}
}
