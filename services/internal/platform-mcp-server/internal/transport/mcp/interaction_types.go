package mcptransport

import (
	"context"

	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
)

const (
	ToolInteractionOwnerInboxList    = "interaction.owner_inbox.list"
	ToolInteractionOwnerInboxGet     = "interaction.owner_inbox.get"
	ToolInteractionOwnerInboxRespond = "interaction.owner_inbox.respond"
)

// InteractionHubClient is the owner route used by interaction MCP tools.
type InteractionHubClient interface {
	InteractionOwnerInboxClient
	InteractionOwnerResponseClient
}

type InteractionOwnerInboxClient interface {
	ListOwnerInboxItems(context.Context, *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error)
	GetOwnerInboxItem(context.Context, *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error)
}

type InteractionOwnerResponseClient interface {
	RecordInteractionResponse(context.Context, *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error)
}

// InteractionCommandMetaInput carries safe command metadata for interaction-hub tools.
type InteractionCommandMetaInput struct {
	CommandID       string                         `json:"command_id,omitempty" jsonschema:"unique command identifier"`
	IdempotencyKey  string                         `json:"idempotency_key,omitempty" jsonschema:"idempotency key scoped by operation and actor"`
	ExpectedVersion *int64                         `json:"expected_version,omitempty" jsonschema:"expected aggregate version for optimistic concurrency"`
	Actor           InteractionActorInput          `json:"actor" jsonschema:"authenticated caller"`
	Reason          string                         `json:"reason" jsonschema:"machine or operator reason for audit"`
	RequestID       string                         `json:"request_id" jsonschema:"request identifier for logs and audit"`
	RequestContext  InteractionRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

// InteractionQueryMetaInput carries safe read metadata for interaction-hub tools.
type InteractionQueryMetaInput struct {
	Actor          InteractionActorInput          `json:"actor" jsonschema:"authenticated caller"`
	RequestID      string                         `json:"request_id" jsonschema:"request identifier for logs and traces"`
	RequestContext InteractionRequestContextInput `json:"request_context" jsonschema:"safe request context"`
}

type InteractionActorInput struct {
	Type string `json:"type" jsonschema:"actor type such as user, service, agent or external_account"`
	ID   string `json:"id" jsonschema:"actor identifier in its owner domain"`
}

type InteractionRequestContextInput struct {
	Source       string `json:"source" jsonschema:"caller surface, for example platform-mcp-server"`
	TraceID      string `json:"trace_id,omitempty" jsonschema:"platform trace identifier"`
	SessionID    string `json:"session_id,omitempty" jsonschema:"user or agent session identifier"`
	ClientIPHash string `json:"client_ip_hash,omitempty" jsonschema:"hashed client address"`
}

type InteractionScopeInput struct {
	Type string `json:"type" jsonschema:"scope type: platform, organization, project, repository or service"`
	Ref  string `json:"ref" jsonschema:"scope identifier owned by another domain"`
}

type InteractionActorRefInput struct {
	RefKind string `json:"ref_kind,omitempty" jsonschema:"actor ref kind, for example user, group, role or service"`
	Ref     string `json:"ref,omitempty" jsonschema:"owner-domain actor ref"`
}

type InteractionExternalRefInput struct {
	RefKind string `json:"ref_kind,omitempty" jsonschema:"external ref kind"`
	Ref     string `json:"ref,omitempty" jsonschema:"owner-domain ref"`
}

type InteractionObjectInput = AgentObjectInput
type InteractionPageInput = AgentPageInput

type ListOwnerInboxInput struct {
	Meta               InteractionQueryMetaInput   `json:"meta" jsonschema:"query metadata"`
	Scope              InteractionScopeInput       `json:"scope" jsonschema:"owner inbox scope"`
	RequestKinds       []string                    `json:"request_kinds,omitempty" jsonschema:"request kind filters: feedback, approval or human_gate"`
	Statuses           []string                    `json:"statuses,omitempty" jsonschema:"status filters: created, routed, waiting, answered, expired, cancelled or failed"`
	SourceOwnerKind    string                      `json:"source_owner_kind,omitempty" jsonschema:"source owner kind filter"`
	SourceOwnerRef     string                      `json:"source_owner_ref,omitempty" jsonschema:"source owner ref filter"`
	AssigneeRef        InteractionActorRefInput    `json:"assignee_ref,omitempty" jsonschema:"assignee filter"`
	ActorRef           string                      `json:"actor_ref,omitempty" jsonschema:"actor ref filter"`
	CorrelationRef     InteractionExternalRefInput `json:"correlation_ref,omitempty" jsonschema:"correlation ref filter"`
	CorrelationID      string                      `json:"correlation_id,omitempty" jsonschema:"correlation identifier"`
	IncludeDiagnostics bool                        `json:"include_diagnostics,omitempty" jsonschema:"include bounded delivery diagnostics"`
	Page               InteractionPageInput        `json:"page,omitempty" jsonschema:"page request"`
}

type GetOwnerInboxInput struct {
	Meta               InteractionQueryMetaInput `json:"meta" jsonschema:"query metadata"`
	RequestID          string                    `json:"request_id" jsonschema:"interaction request identifier"`
	Scope              InteractionScopeInput     `json:"scope" jsonschema:"owner inbox scope"`
	AssigneeRef        InteractionActorRefInput  `json:"assignee_ref,omitempty" jsonschema:"assignee context"`
	IncludeDiagnostics bool                      `json:"include_diagnostics,omitempty" jsonschema:"include bounded delivery diagnostics"`
}

type RespondOwnerInboxInput struct {
	Meta                InteractionCommandMetaInput `json:"meta" jsonschema:"command metadata"`
	RequestID           string                      `json:"request_id" jsonschema:"interaction request identifier"`
	ResponseAction      string                      `json:"response_action" jsonschema:"response action: answer, approve, reject, defer, acknowledge or custom"`
	RespondedByActorRef string                      `json:"responded_by_actor_ref" jsonschema:"verified platform actor or external subject ref"`
	ResponseSummary     string                      `json:"response_summary,omitempty" jsonschema:"short safe response summary"`
	ResponseObject      InteractionObjectInput      `json:"response_object,omitempty" jsonschema:"sanitized response object ref"`
	SourceKind          string                      `json:"source_kind" jsonschema:"source kind: mcp, web_console, channel_callback, system or service"`
	SourceRef           string                      `json:"source_ref,omitempty" jsonschema:"safe callback, message or command ref"`
	OwnerDecisionRef    string                      `json:"owner_decision_ref,omitempty" jsonschema:"owner-domain decision ref when already recorded"`
}

type OwnerInboxOutput struct {
	Item OwnerInboxItemSummary `json:"item" jsonschema:"owner inbox item"`
}

type OwnerInboxListOutput struct {
	Items []OwnerInboxItemSummary `json:"items" jsonschema:"owner inbox items"`
	Page  PageSummary             `json:"page" jsonschema:"page metadata"`
}

type OwnerInboxResponseOutput struct {
	Request  OwnerInboxRequestSummary  `json:"request" jsonschema:"interaction request summary"`
	Response OwnerInboxResponseSummary `json:"response" jsonschema:"interaction response summary"`
}

type OwnerInboxItemSummary struct {
	RequestID         string                           `json:"request_id" jsonschema:"interaction request identifier"`
	RequestKind       string                           `json:"request_kind" jsonschema:"request kind"`
	RequestStatus     string                           `json:"request_status" jsonschema:"request status"`
	Scope             InteractionScopeSummary          `json:"scope" jsonschema:"scope"`
	Requester         InteractionSourceOwnerSummary    `json:"requester" jsonschema:"source owner"`
	DecisionOwner     *InteractionDecisionOwnerSummary `json:"decision_owner,omitempty" jsonschema:"decision owner"`
	AssigneeRefs      []InteractionActorRefSummary     `json:"assignee_refs,omitempty" jsonschema:"assignee refs"`
	ContextRefs       []InteractionExternalRefSummary  `json:"context_refs,omitempty" jsonschema:"context refs"`
	Title             string                           `json:"title,omitempty" jsonschema:"short title"`
	Summary           string                           `json:"summary,omitempty" jsonschema:"short safe summary"`
	DeadlineAt        string                           `json:"deadline_at,omitempty" jsonschema:"deadline timestamp"`
	ReminderPolicyRef string                           `json:"reminder_policy_ref,omitempty" jsonschema:"reminder policy ref"`
	DeliverySummary   OwnerInboxDeliverySummary        `json:"delivery_summary" jsonschema:"delivery summary"`
	LatestCallback    *OwnerInboxCallbackSummary       `json:"latest_callback,omitempty" jsonschema:"latest callback summary"`
	LatestResponse    *OwnerInboxResponseSummary       `json:"latest_response,omitempty" jsonschema:"latest response summary"`
	CreatedAt         string                           `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt         string                           `json:"updated_at" jsonschema:"updated timestamp"`
	ResolvedAt        string                           `json:"resolved_at,omitempty" jsonschema:"resolved timestamp"`
	Version           int64                            `json:"version" jsonschema:"version"`
	AllowedActions    []InteractionActionSummary       `json:"allowed_actions,omitempty" jsonschema:"allowed actions"`
}

type OwnerInboxRequestSummary struct {
	ID            string                           `json:"id" jsonschema:"interaction request identifier"`
	RequestKind   string                           `json:"request_kind" jsonschema:"request kind"`
	Scope         InteractionScopeSummary          `json:"scope" jsonschema:"scope"`
	SourceOwner   InteractionSourceOwnerSummary    `json:"source_owner" jsonschema:"source owner"`
	DecisionOwner *InteractionDecisionOwnerSummary `json:"decision_owner,omitempty" jsonschema:"decision owner"`
	TargetRefs    []InteractionActorRefSummary     `json:"target_refs,omitempty" jsonschema:"target refs"`
	ContextRefs   []InteractionExternalRefSummary  `json:"context_refs,omitempty" jsonschema:"context refs"`
	PromptSummary string                           `json:"prompt_summary,omitempty" jsonschema:"bounded prompt summary"`
	Status        string                           `json:"status" jsonschema:"request status"`
	Version       int64                            `json:"version" jsonschema:"version"`
	CreatedAt     string                           `json:"created_at" jsonschema:"created timestamp"`
	UpdatedAt     string                           `json:"updated_at" jsonschema:"updated timestamp"`
	ResolvedAt    string                           `json:"resolved_at,omitempty" jsonschema:"resolved timestamp"`
}

type OwnerInboxDeliverySummary struct {
	AttemptCount            int32  `json:"attempt_count" jsonschema:"attempt count"`
	LatestDeliveryAttemptID string `json:"latest_delivery_attempt_id,omitempty" jsonschema:"latest delivery attempt identifier"`
	LatestDeliveryID        string `json:"latest_delivery_id,omitempty" jsonschema:"latest delivery identifier"`
	LatestStatus            string `json:"latest_status" jsonschema:"latest delivery status"`
	LatestErrorCode         string `json:"latest_error_code,omitempty" jsonschema:"safe delivery error code"`
	LatestErrorClass        string `json:"latest_error_class" jsonschema:"delivery error class"`
	NextRetryAt             string `json:"next_retry_at,omitempty" jsonschema:"next retry timestamp"`
	LatestUpdatedAt         string `json:"latest_updated_at,omitempty" jsonschema:"latest update timestamp"`
	RouteID                 string `json:"route_id,omitempty" jsonschema:"route identifier"`
	ChannelMessageRef       string `json:"channel_message_ref,omitempty" jsonschema:"channel message ref"`
}

type OwnerInboxCallbackSummary struct {
	CallbackRef      string `json:"callback_ref" jsonschema:"callback ref"`
	CallbackID       string `json:"callback_id" jsonschema:"callback identifier"`
	DeliveryID       string `json:"delivery_id,omitempty" jsonschema:"delivery identifier"`
	SignatureStatus  string `json:"signature_status" jsonschema:"signature status"`
	ProcessingStatus string `json:"processing_status" jsonschema:"processing status"`
	ActorRef         string `json:"actor_ref,omitempty" jsonschema:"actor ref"`
	Action           string `json:"action,omitempty" jsonschema:"safe action key"`
	ErrorCode        string `json:"error_code,omitempty" jsonschema:"safe error code"`
	ReceivedAt       string `json:"received_at" jsonschema:"received timestamp"`
	GatewayRef       string `json:"gateway_ref,omitempty" jsonschema:"gateway ref"`
	CorrelationID    string `json:"correlation_id,omitempty" jsonschema:"correlation identifier"`
}

type OwnerInboxResponseSummary struct {
	ResponseID             string                    `json:"response_id" jsonschema:"response identifier"`
	RequestID              string                    `json:"request_id,omitempty" jsonschema:"request identifier"`
	ResponseAction         string                    `json:"response_action" jsonschema:"response action"`
	RespondedByActorRef    string                    `json:"responded_by_actor_ref" jsonschema:"responded-by actor ref"`
	SourceKind             string                    `json:"source_kind" jsonschema:"response source kind"`
	SourceRef              string                    `json:"source_ref,omitempty" jsonschema:"safe source ref"`
	OwnerDecisionRef       string                    `json:"owner_decision_ref,omitempty" jsonschema:"owner decision ref"`
	CreatedAt              string                    `json:"created_at" jsonschema:"created timestamp"`
	ResponseSummary        string                    `json:"response_summary,omitempty" jsonschema:"short safe response summary"`
	ResponseSummaryDigest  string                    `json:"response_summary_digest,omitempty" jsonschema:"response summary digest"`
	ResponseObject         *InteractionObjectSummary `json:"response_object,omitempty" jsonschema:"sanitized response object ref"`
	InteractionResponseRef string                    `json:"interaction_response_ref,omitempty" jsonschema:"interaction response ref"`
}

type InteractionScopeSummary = InteractionScopeInput
type InteractionActorRefSummary = InteractionActorRefInput
type InteractionExternalRefSummary = InteractionExternalRefInput
type InteractionObjectSummary = InteractionObjectInput

type InteractionSourceOwnerSummary struct {
	Kind string `json:"kind" jsonschema:"source owner kind"`
	Ref  string `json:"ref,omitempty" jsonschema:"source owner ref"`
}

type InteractionDecisionOwnerSummary struct {
	OwnerKind        string `json:"owner_kind" jsonschema:"decision owner kind"`
	OwnerRequestRef  string `json:"owner_request_ref" jsonschema:"owner request ref"`
	OwnerDecisionRef string `json:"owner_decision_ref,omitempty" jsonschema:"owner decision ref"`
}

type InteractionActionSummary struct {
	ActionKey        string `json:"action_key" jsonschema:"action key"`
	LabelTemplateRef string `json:"label_template_ref,omitempty" jsonschema:"label template ref"`
	IsTerminal       bool   `json:"is_terminal" jsonschema:"whether action resolves request"`
}
