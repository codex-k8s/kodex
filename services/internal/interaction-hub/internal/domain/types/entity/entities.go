package entity

import (
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
)

type ConversationThread struct {
	ID              uuid.UUID
	Scope           value.ScopeRef
	ThreadKind      enum.ConversationThreadKind
	PrimaryActorRef string
	SourceKind      enum.ConversationSourceKind
	SourceRef       string
	Status          enum.ConversationThreadStatus
	LatestMessageID *uuid.UUID
	CorrelationID   string
	RetentionClass  string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ClosedAt        *time.Time
}

type ConversationMessage struct {
	ID           uuid.UUID
	ThreadID     uuid.UUID
	MessageKind  enum.ConversationMessageKind
	AuthorRef    string
	BodySummary  string
	BodyObject   value.ObjectRef
	BodyDigest   string
	Locale       string
	SafeMetadata map[string]string
	CreatedAt    time.Time
}

type InteractionRequest struct {
	ID                uuid.UUID
	RequestKind       enum.InteractionRequestKind
	Scope             value.ScopeRef
	ThreadID          *uuid.UUID
	SourceOwner       value.SourceOwnerRef
	Ingress           value.IngressRef
	DecisionOwner     value.DecisionOwnerRef
	TargetRefs        []value.ActorRef
	ContextRefs       []value.ExternalRef
	PromptSummary     string
	PromptObject      value.ObjectRef
	AllowedActions    []value.InteractionAction
	RiskClass         enum.InteractionRiskClass
	Status            enum.InteractionRequestStatus
	DeadlineAt        *time.Time
	ReminderPolicyRef string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ResolvedAt        *time.Time
}

type InteractionResponse struct {
	ID                  uuid.UUID
	RequestID           uuid.UUID
	ResponseAction      enum.InteractionResponseAction
	RespondedByActorRef string
	ResponseSummary     string
	ResponseObject      value.ObjectRef
	SourceKind          enum.InteractionResponseSourceKind
	SourceRef           string
	OwnerDecisionRef    string
	CreatedAt           time.Time
}

type Notification struct {
	ID                    uuid.UUID
	Scope                 value.ScopeRef
	NotificationKind      enum.NotificationKind
	RequestID             *uuid.UUID
	SubscriptionID        *uuid.UUID
	RecipientRefs         []value.ActorRef
	MessageTemplateRef    string
	MessageTitle          string
	MessageSummary        string
	BodyPreview           string
	Priority              enum.NotificationPriority
	Status                enum.NotificationStatus
	SourceOwner           value.SourceOwnerRef
	Ingress               value.IngressRef
	ContextRefs           []value.ExternalRef
	ChannelHintRefs       []value.ExternalRef
	NotificationPolicyRef string
	CreatedAt             time.Time
	UpdatedAt             time.Time
	ExpiresAt             *time.Time
}

type Subscription struct {
	ID                      uuid.UUID
	Scope                   value.ScopeRef
	SubscriberRef           value.ActorRef
	EventFilterJSON         string
	DeliveryPreferencesJSON string
	Status                  enum.SubscriptionStatus
	Version                 int64
	SourceOwner             value.SourceOwnerRef
	ChannelHintRefs         []value.ExternalRef
	SubscriptionPolicyRef   string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type DeliveryRoute struct {
	ID                     uuid.UUID
	Scope                  value.ScopeRef
	SurfaceKind            enum.DeliverySurfaceKind
	ChannelCapabilityRef   string
	PackageInstallationRef string
	PackageVersionRef      string
	RoutingPolicyRef       string
	CallbackRouteRef       string
	RuntimeRef             string
	Status                 enum.DeliveryRouteStatus
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type DeliveryAttempt struct {
	ID                     uuid.UUID
	Target                 value.DeliveryTarget
	RouteID                uuid.UUID
	DeliveryID             string
	DeliveryKind           enum.DeliveryKind
	Status                 enum.DeliveryAttemptStatus
	ChannelMessageRef      string
	AttemptNumber          int32
	NextRetryAt            *time.Time
	ErrorCode              string
	ErrorClass             enum.DeliveryErrorClass
	PayloadDigest          string
	ResultFingerprint      string
	ChannelCapabilityRef   string
	PackageInstallationRef string
	PackageVersionRef      string
	DeliveryCommandRef     string
	CallbackRef            string
	CallbackRouteRef       string
	RuntimeRef             string
	RuntimeJobRef          string
	RoutingPolicyRef       string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	SentAt                 *time.Time
}

type ChannelCallback struct {
	ID                  uuid.UUID
	CallbackID          string
	DeliveryID          string
	DeliveryAttemptID   *uuid.UUID
	RequestID           *uuid.UUID
	SourceRouteID       *uuid.UUID
	ActorRef            string
	Action              string
	CallbackSummary     string
	CallbackObject      value.ObjectRef
	SignatureStatus     enum.CallbackSignatureStatus
	ProcessingStatus    enum.CallbackProcessingStatus
	ErrorCode           string
	ReceivedAt          time.Time
	CreatedAt           time.Time
	CallbackRouteRef    string
	GatewayRef          string
	CorrelationID       string
	CallbackFingerprint string
}

type CommandResult struct {
	Key                string
	CommandID          uuid.UUID
	IdempotencyKey     string
	ActorRef           string
	Operation          enum.Operation
	AggregateType      string
	AggregateID        uuid.UUID
	RequestFingerprint string
	ResultPayload      []byte
	CreatedAt          time.Time
}

type OutboxEvent struct {
	outboxlib.Event
	PublishedAt         *time.Time
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailedPermanentlyAt *time.Time
	FailureKind         string
	LastError           string
}
