package casters

import (
	"github.com/google/uuid"

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
	return queryIDInput(input, queryIDAdapter[*interactionsv1.GetConversationThreadRequest, interactionservice.GetConversationThreadInput]{
		metaInput: (*interactionsv1.GetConversationThreadRequest).GetMeta,
		idInput:   (*interactionsv1.GetConversationThreadRequest).GetThreadId,
		build:     conversationThreadReadInput,
	})
}

func conversationThreadReadInput(meta value.QueryMeta, threadID uuid.UUID) interactionservice.GetConversationThreadInput {
	return interactionservice.GetConversationThreadInput{Meta: meta, ThreadID: threadID}
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
	return &interactionsv1.ListConversationMessagesResponse{Messages: castSlice(messages, ConversationMessage), Page: PageResponse(page)}
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
	return domainEnumValue[enum.ConversationThreadKind](input, "CONVERSATION_THREAD_KIND_")
}

func ThreadKindProto(input enum.ConversationThreadKind) interactionsv1.ConversationThreadKind {
	return protoEnumValue(input, interactionsv1.ConversationThreadKind_value, "CONVERSATION_THREAD_KIND_", interactionsv1.ConversationThreadKind_CONVERSATION_THREAD_KIND_UNSPECIFIED)
}

func SourceKind(input interactionsv1.ConversationSourceKind) enum.ConversationSourceKind {
	return domainEnumValue[enum.ConversationSourceKind](input, "CONVERSATION_SOURCE_KIND_")
}

func SourceKindProto(input enum.ConversationSourceKind) interactionsv1.ConversationSourceKind {
	return protoEnumValue(input, interactionsv1.ConversationSourceKind_value, "CONVERSATION_SOURCE_KIND_", interactionsv1.ConversationSourceKind_CONVERSATION_SOURCE_KIND_UNSPECIFIED)
}

func ThreadStatusProto(input enum.ConversationThreadStatus) interactionsv1.ConversationThreadStatus {
	return protoEnumValue(input, interactionsv1.ConversationThreadStatus_value, "CONVERSATION_THREAD_STATUS_", interactionsv1.ConversationThreadStatus_CONVERSATION_THREAD_STATUS_UNSPECIFIED)
}

func MessageKind(input interactionsv1.ConversationMessageKind) enum.ConversationMessageKind {
	return domainEnumValue[enum.ConversationMessageKind](input, "CONVERSATION_MESSAGE_KIND_")
}

func MessageKindProto(input enum.ConversationMessageKind) interactionsv1.ConversationMessageKind {
	return protoEnumValue(input, interactionsv1.ConversationMessageKind_value, "CONVERSATION_MESSAGE_KIND_", interactionsv1.ConversationMessageKind_CONVERSATION_MESSAGE_KIND_UNSPECIFIED)
}
