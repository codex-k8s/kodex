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
