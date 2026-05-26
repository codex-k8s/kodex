package service

import (
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

type Config struct {
	Clock         value.Clock
	UUIDGenerator value.UUIDGenerator
}

type CreateConversationThreadInput struct {
	Meta            value.CommandMeta
	Scope           value.ScopeRef
	ThreadKind      enum.ConversationThreadKind
	PrimaryActorRef string
	SourceKind      enum.ConversationSourceKind
	SourceRef       string
	CorrelationID   string
	RetentionClass  string
}

type RecordConversationMessageInput struct {
	Meta         value.CommandMeta
	ThreadID     uuid.UUID
	MessageKind  enum.ConversationMessageKind
	AuthorRef    string
	BodySummary  string
	BodyObject   value.ObjectRef
	BodyDigest   string
	Locale       string
	SafeMetadata map[string]string
}

type GetConversationThreadInput struct {
	Meta     value.QueryMeta
	ThreadID uuid.UUID
}

type ListConversationMessagesInput struct {
	Meta     value.QueryMeta
	ThreadID uuid.UUID
	Page     value.PageRequest
}
