package service

import (
	"time"

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

type InteractionRequestDraftInput struct {
	Scope             value.ScopeRef
	ThreadID          uuid.UUID
	SourceOwner       value.SourceOwnerRef
	Ingress           value.IngressRef
	DecisionOwner     value.DecisionOwnerRef
	TargetRefs        []value.ActorRef
	ContextRefs       []value.ExternalRef
	PromptSummary     string
	PromptObject      value.ObjectRef
	AllowedActions    []value.InteractionAction
	RiskClass         enum.InteractionRiskClass
	DeadlineAt        *time.Time
	ReminderPolicyRef string
}

type RequestFeedbackInput struct {
	Meta    value.CommandMeta
	Request InteractionRequestDraftInput
}

type RequestApprovalInput struct {
	Meta    value.CommandMeta
	Request InteractionRequestDraftInput
}

type RequestHumanGateInput struct {
	Meta    value.CommandMeta
	Request InteractionRequestDraftInput
}

type RecordInteractionResponseInput struct {
	Meta                value.CommandMeta
	RequestID           uuid.UUID
	ResponseAction      enum.InteractionResponseAction
	RespondedByActorRef string
	ResponseSummary     string
	ResponseObject      value.ObjectRef
	SourceKind          enum.InteractionResponseSourceKind
	SourceRef           string
	OwnerDecisionRef    string
}

type CancelInteractionRequestInput struct {
	Meta      value.CommandMeta
	RequestID uuid.UUID
}

type ExpireInteractionRequestsInput struct {
	Meta           value.CommandMeta
	Scope          value.ScopeRef
	DeadlineBefore *time.Time
	Limit          int32
}

type ExpireInteractionRequestsResult struct {
	ExpiredRequestIDs []uuid.UUID
}

type GetInteractionRequestInput struct {
	Meta      value.QueryMeta
	RequestID uuid.UUID
}

type ListInteractionRequestsInput struct {
	Meta            value.QueryMeta
	Scope           value.ScopeRef
	RequestKind     enum.InteractionRequestKind
	Status          enum.InteractionRequestStatus
	SourceOwnerKind enum.SourceOwnerKind
	SourceOwnerRef  string
	DeadlineBefore  *time.Time
	Page            value.PageRequest
}

type RequestNotificationInput struct {
	Meta                  value.CommandMeta
	Scope                 value.ScopeRef
	NotificationKind      enum.NotificationKind
	RequestID             uuid.UUID
	SubscriptionID        uuid.UUID
	RecipientRefs         []value.ActorRef
	MessageTemplateRef    string
	MessageTitle          string
	MessageSummary        string
	BodyPreview           string
	Priority              enum.NotificationPriority
	ExpiresAt             *time.Time
	SourceOwner           value.SourceOwnerRef
	Ingress               value.IngressRef
	ContextRefs           []value.ExternalRef
	ChannelHintRefs       []value.ExternalRef
	NotificationPolicyRef string
}

type UpsertSubscriptionInput struct {
	Meta                    value.CommandMeta
	SubscriptionID          uuid.UUID
	Scope                   value.ScopeRef
	SubscriberRef           value.ActorRef
	EventFilterJSON         string
	DeliveryPreferencesJSON string
	Status                  enum.SubscriptionStatus
	SourceOwner             value.SourceOwnerRef
	ChannelHintRefs         []value.ExternalRef
	SubscriptionPolicyRef   string
}

type DisableSubscriptionInput struct {
	Meta           value.CommandMeta
	SubscriptionID uuid.UUID
}

type ListSubscriptionsInput struct {
	Meta          value.QueryMeta
	Scope         value.ScopeRef
	SubscriberRef string
	Status        enum.SubscriptionStatus
	Page          value.PageRequest
}
