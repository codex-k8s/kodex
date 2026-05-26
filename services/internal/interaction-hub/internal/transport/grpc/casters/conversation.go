package casters

import (
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	interactionservice "github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

func CreateConversationThreadInput(input *interactionsv1.CreateConversationThreadRequest) (interactionservice.CreateConversationThreadInput, error) {
	if input == nil {
		return interactionservice.CreateConversationThreadInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.CreateConversationThreadInput{}, err
	}
	scope, err := ScopeRef(input.GetScope())
	if err != nil {
		return interactionservice.CreateConversationThreadInput{}, err
	}
	return interactionservice.CreateConversationThreadInput{
		Meta:            meta,
		Scope:           scope,
		ThreadKind:      ThreadKind(input.GetThreadKind()),
		PrimaryActorRef: input.GetPrimaryActorRef(),
		SourceKind:      SourceKind(input.GetSourceKind()),
		SourceRef:       input.GetSourceRef(),
		CorrelationID:   input.GetCorrelationId(),
		RetentionClass:  input.GetRetentionClass(),
	}, nil
}

func RecordConversationMessageInput(input *interactionsv1.RecordConversationMessageRequest) (interactionservice.RecordConversationMessageInput, error) {
	if input == nil {
		return interactionservice.RecordConversationMessageInput{}, errs.ErrInvalidArgument
	}
	meta, err := CommandMeta(input.GetMeta())
	if err != nil {
		return interactionservice.RecordConversationMessageInput{}, err
	}
	threadID, err := ParseUUID(input.GetThreadId())
	if err != nil {
		return interactionservice.RecordConversationMessageInput{}, err
	}
	return interactionservice.RecordConversationMessageInput{
		Meta:         meta,
		ThreadID:     threadID,
		MessageKind:  MessageKind(input.GetMessageKind()),
		AuthorRef:    input.GetAuthorRef(),
		BodySummary:  input.GetBodySummary(),
		BodyObject:   ObjectRef(input.GetBodyObject()),
		BodyDigest:   input.GetBodyDigest(),
		Locale:       input.GetLocale(),
		SafeMetadata: input.GetSafeMetadata(),
	}, nil
}

func GetConversationThreadInput(input *interactionsv1.GetConversationThreadRequest) (interactionservice.GetConversationThreadInput, error) {
	if input == nil {
		return interactionservice.GetConversationThreadInput{}, errs.ErrInvalidArgument
	}
	threadID, err := ParseUUID(input.GetThreadId())
	if err != nil {
		return interactionservice.GetConversationThreadInput{}, err
	}
	return interactionservice.GetConversationThreadInput{Meta: QueryMeta(input.GetMeta()), ThreadID: threadID}, nil
}

func ListConversationMessagesInput(input *interactionsv1.ListConversationMessagesRequest) (interactionservice.ListConversationMessagesInput, error) {
	if input == nil {
		return interactionservice.ListConversationMessagesInput{}, errs.ErrInvalidArgument
	}
	threadID, err := ParseUUID(input.GetThreadId())
	if err != nil {
		return interactionservice.ListConversationMessagesInput{}, err
	}
	return interactionservice.ListConversationMessagesInput{Meta: QueryMeta(input.GetMeta()), ThreadID: threadID, Page: PageRequest(input.GetPage())}, nil
}

func ConversationThreadResponse(thread entity.ConversationThread) *interactionsv1.ConversationThreadResponse {
	return &interactionsv1.ConversationThreadResponse{Thread: ConversationThread(thread)}
}

func ConversationMessageResponse(message entity.ConversationMessage) *interactionsv1.ConversationMessageResponse {
	return &interactionsv1.ConversationMessageResponse{Message: ConversationMessage(message)}
}

func ListConversationMessagesResponse(messages []entity.ConversationMessage, page value.PageResult) *interactionsv1.ListConversationMessagesResponse {
	items := make([]*interactionsv1.ConversationMessage, 0, len(messages))
	for _, message := range messages {
		items = append(items, ConversationMessage(message))
	}
	return &interactionsv1.ListConversationMessagesResponse{Messages: items, Page: PageResponse(page)}
}

func ConversationThread(thread entity.ConversationThread) *interactionsv1.ConversationThread {
	return &interactionsv1.ConversationThread{
		Id:              thread.ID.String(),
		Scope:           &interactionsv1.ScopeRef{Type: ScopeTypeProto(thread.Scope.Type), Ref: thread.Scope.Ref},
		ThreadKind:      ThreadKindProto(thread.ThreadKind),
		PrimaryActorRef: OptionalString(thread.PrimaryActorRef),
		SourceKind:      SourceKindProto(thread.SourceKind),
		SourceRef:       OptionalString(thread.SourceRef),
		Status:          ThreadStatusProto(thread.Status),
		LatestMessageId: OptionalUUIDProto(thread.LatestMessageID),
		CorrelationId:   thread.CorrelationID,
		RetentionClass:  thread.RetentionClass,
		Version:         thread.Version,
		CreatedAt:       TimeProto(thread.CreatedAt),
		UpdatedAt:       TimeProto(thread.UpdatedAt),
		ClosedAt:        OptionalTimeProto(thread.ClosedAt),
	}
}

func ConversationMessage(message entity.ConversationMessage) *interactionsv1.ConversationMessage {
	return &interactionsv1.ConversationMessage{
		Id:           message.ID.String(),
		ThreadId:     message.ThreadID.String(),
		MessageKind:  MessageKindProto(message.MessageKind),
		AuthorRef:    message.AuthorRef,
		BodySummary:  OptionalString(message.BodySummary),
		BodyObject:   ObjectRefProto(message.BodyObject),
		BodyDigest:   OptionalString(message.BodyDigest),
		Locale:       OptionalString(message.Locale),
		SafeMetadata: message.SafeMetadata,
		CreatedAt:    TimeProto(message.CreatedAt),
	}
}

func ThreadKind(input interactionsv1.ConversationThreadKind) enum.ConversationThreadKind {
	switch input {
	case interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_USER_DIALOG:
		return enum.ConversationThreadKindUserDialog
	case interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_OWNER_FEEDBACK:
		return enum.ConversationThreadKindOwnerFeedback
	case interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_APPROVAL:
		return enum.ConversationThreadKindApproval
	case interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_HUMAN_GATE:
		return enum.ConversationThreadKindHumanGate
	case interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_NOTIFICATION:
		return enum.ConversationThreadKindNotification
	case interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_OPS:
		return enum.ConversationThreadKindOps
	default:
		return ""
	}
}

func ThreadKindProto(input enum.ConversationThreadKind) interactionsv1.ConversationThreadKind {
	switch input {
	case enum.ConversationThreadKindUserDialog:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_USER_DIALOG
	case enum.ConversationThreadKindOwnerFeedback:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_OWNER_FEEDBACK
	case enum.ConversationThreadKindApproval:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_APPROVAL
	case enum.ConversationThreadKindHumanGate:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_HUMAN_GATE
	case enum.ConversationThreadKindNotification:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_NOTIFICATION
	case enum.ConversationThreadKindOps:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_OPS
	default:
		return interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_UNSPECIFIED
	}
}

func SourceKind(input interactionsv1.ConversationSourceKind) enum.ConversationSourceKind {
	switch input {
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_WEB_CONSOLE:
		return enum.ConversationSourceKindWebConsole
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_VOICE:
		return enum.ConversationSourceKindVoice
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_MCP:
		return enum.ConversationSourceKindMCP
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_PROVIDER:
		return enum.ConversationSourceKindProvider
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_CHANNEL_PACKAGE:
		return enum.ConversationSourceKindChannelPackage
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_CODEX_HOOK:
		return enum.ConversationSourceKindCodexHook
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_SYSTEM:
		return enum.ConversationSourceKindSystem
	case interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_SERVICE:
		return enum.ConversationSourceKindService
	default:
		return ""
	}
}

func SourceKindProto(input enum.ConversationSourceKind) interactionsv1.ConversationSourceKind {
	switch input {
	case enum.ConversationSourceKindWebConsole:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_WEB_CONSOLE
	case enum.ConversationSourceKindVoice:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_VOICE
	case enum.ConversationSourceKindMCP:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_MCP
	case enum.ConversationSourceKindProvider:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_PROVIDER
	case enum.ConversationSourceKindChannelPackage:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_CHANNEL_PACKAGE
	case enum.ConversationSourceKindCodexHook:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_CODEX_HOOK
	case enum.ConversationSourceKindSystem:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_SYSTEM
	case enum.ConversationSourceKindService:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_SERVICE
	default:
		return interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_UNSPECIFIED
	}
}

func ThreadStatusProto(input enum.ConversationThreadStatus) interactionsv1.ConversationThreadStatus {
	switch input {
	case enum.ConversationThreadStatusOpen:
		return interactionsv1.ConversationThreadStatus_CONVERSATION_THREAD_STATUS_OPEN
	case enum.ConversationThreadStatusWaiting:
		return interactionsv1.ConversationThreadStatus_CONVERSATION_THREAD_STATUS_WAITING
	case enum.ConversationThreadStatusClosed:
		return interactionsv1.ConversationThreadStatus_CONVERSATION_THREAD_STATUS_CLOSED
	case enum.ConversationThreadStatusArchived:
		return interactionsv1.ConversationThreadStatus_CONVERSATION_THREAD_STATUS_ARCHIVED
	default:
		return interactionsv1.ConversationThreadStatus_CONVERSATION_THREAD_STATUS_UNSPECIFIED
	}
}

func MessageKind(input interactionsv1.ConversationMessageKind) enum.ConversationMessageKind {
	switch input {
	case interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_USER_TEXT:
		return enum.ConversationMessageKindUserText
	case interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_VOICE_TRANSCRIPT:
		return enum.ConversationMessageKindVoiceTranscript
	case interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_AGENT_TEXT:
		return enum.ConversationMessageKindAgentText
	case interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_SYSTEM_NOTICE:
		return enum.ConversationMessageKindSystemNotice
	case interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_RESPONSE_SUMMARY:
		return enum.ConversationMessageKindResponseSummary
	case interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_CALLBACK_SUMMARY:
		return enum.ConversationMessageKindCallbackSummary
	default:
		return ""
	}
}

func MessageKindProto(input enum.ConversationMessageKind) interactionsv1.ConversationMessageKind {
	switch input {
	case enum.ConversationMessageKindUserText:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_USER_TEXT
	case enum.ConversationMessageKindVoiceTranscript:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_VOICE_TRANSCRIPT
	case enum.ConversationMessageKindAgentText:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_AGENT_TEXT
	case enum.ConversationMessageKindSystemNotice:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_SYSTEM_NOTICE
	case enum.ConversationMessageKindResponseSummary:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_RESPONSE_SUMMARY
	case enum.ConversationMessageKindCallbackSummary:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_CALLBACK_SUMMARY
	default:
		return interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_UNSPECIFIED
	}
}
